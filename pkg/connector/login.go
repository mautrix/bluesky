// mautrix-bluesky - A Matrix-Bluesky puppeting bridge.
// Copyright (C) 2024 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/rs/zerolog"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

var loginFlows = []bridgev2.LoginFlow{{
	Name:        "Username & password",
	Description: "Log in by entering your Bluesky username and password",
	ID:          "password",
}}

func (b *BlueskyConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return loginFlows
}

func (b *BlueskyConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	if flowID != "password" {
		return nil, bridgev2.ErrInvalidLoginFlowID
	}
	return &PasswordLogin{User: user}, nil
}

type PasswordLogin struct {
	User *bridgev2.User
}

var _ bridgev2.LoginProcessUserInput = (*PasswordLogin)(nil)

func (p *PasswordLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       "fi.mau.bluesky.password",
		Instructions: "",
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{{
				Type:        bridgev2.LoginInputFieldTypeDomain,
				ID:          "domain",
				Name:        "Server",
				Description: "Bluesky server. The default server is `bsky.social`",
			}, {
				Type:        bridgev2.LoginInputFieldTypeUsername,
				ID:          "username",
				Name:        "Username or email",
				Description: "Bluesky account handle or email address",
			}, {
				Type:        bridgev2.LoginInputFieldTypePassword,
				ID:          "password",
				Name:        "Password",
				Description: "Bluesky account password",
			}},
		},
	}, nil
}

func (p *PasswordLogin) Cancel() {}

func parseDIDDoc(doc *any) (*identity.Identity, error) {
	docBytes, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DID doc: %w", err)
	}
	var parsedDoc identity.DIDDocument
	err = json.Unmarshal(docBytes, &parsedDoc)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal DID doc: %w", err)
	}
	ident := identity.ParseIdentity(&parsedDoc)
	return &ident, nil
}

func (p *PasswordLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	cli := &xrpc.Client{
		Client:    util.RobustHTTPClient(),
		Host:      fmt.Sprintf("https://%s", input["domain"]),
		UserAgent: &mautrix.DefaultUserAgent,
	}
	resp, err := atproto.ServerCreateSession(ctx, cli, &atproto.ServerCreateSession_Input{
		AuthFactorToken: nil,
		Identifier:      input["username"],
		Password:        input["password"],
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	ident, err := parseDIDDoc(resp.DidDoc)
	if err != nil {
		return nil, err
	}
	pdsEndpoint := ident.PDSEndpoint()
	if pdsEndpoint == "" {
		pdsEndpoint = cli.Host
	} else {
		zerolog.Ctx(ctx).Debug().Str("new_pds", pdsEndpoint).Msg("Login response contained PDS endpoint")
	}
	ul, err := p.User.NewLogin(ctx, &database.UserLogin{
		ID:         makeUserLoginID(resp.Did),
		RemoteName: resp.Handle,
		RemoteProfile: status.RemoteProfile{
			Email:    ptr.Val(resp.Email),
			Username: resp.Handle,
		},
		Metadata: &UserLoginMetadata{
			Host: pdsEndpoint,
			Auth: &xrpc.AuthInfo{
				AccessJwt:  resp.AccessJwt,
				RefreshJwt: resp.RefreshJwt,
				Handle:     resp.Handle,
				Did:        resp.Did,
			},
		},
	}, &bridgev2.NewLoginParams{
		DeleteOnConflict: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to save new login: %w", err)
	}
	ul.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting})
	go func(ctx context.Context) {
		bc := ul.Client.(*BlueskyClient)
		err = bc.fetchInbox(ctx)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to fetch inbox after login")
		}
		bc.startPolling()
	}(context.WithoutCancel(ctx))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       "fi.mau.bluesky.complete",
		Instructions: fmt.Sprintf("Successfully logged in as %s", resp.Handle),
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}
