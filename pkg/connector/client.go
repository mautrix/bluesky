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
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/chat"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridge/status"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
)

func (b *BlueskyConnector) LoadUserLogin(ctx context.Context, login *bridgev2.UserLogin) error {
	meta := login.Metadata.(*UserLoginMetadata)
	x := &xrpc.Client{
		Client:    util.RobustHTTPClient(),
		Host:      meta.Host,
		Auth:      meta.Auth,
		UserAgent: &mautrix.DefaultUserAgent,
	}
	chatX := &xrpc.Client{
		Client:    x.Client,
		Host:      x.Host,
		Auth:      x.Auth,
		UserAgent: x.UserAgent,
		Headers: map[string]string{
			"Atproto-Proxy": "did:web:api.bsky.chat#bsky_chat",
		},
	}
	login.Client = &BlueskyClient{
		UserLogin: login,
		Main:      b,
		XRPC:      x,
		ChatRPC:   chatX,
	}
	return nil
}

type BlueskyClient struct {
	UserLogin *bridgev2.UserLogin
	Main      *BlueskyConnector
	XRPC      *xrpc.Client
	ChatRPC   *xrpc.Client

	stopPolling atomic.Pointer[context.CancelFunc]
}

var _ bridgev2.NetworkAPI = (*BlueskyClient)(nil)

func (b *BlueskyClient) Connect(ctx context.Context) {
	b.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting})
	err := b.refreshToken(ctx)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to refresh token")
		b.UserLogin.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      "bsky-token-refresh-failed",
		})
		return
	}
	err = b.fetchInbox(ctx)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to fetch inbox during startup")
	}
	go b.startPolling()
}

func (b *BlueskyClient) refreshToken(ctx context.Context) error {
	// The client is dumb and doesn't know how to use the refresh token itself,
	// so make a new client that has the refresh token in the access token slot.
	refreshClient := &xrpc.Client{
		Client: b.XRPC.Client,
		Auth: &xrpc.AuthInfo{
			AccessJwt:  b.XRPC.Auth.RefreshJwt,
			RefreshJwt: b.XRPC.Auth.RefreshJwt,
			Handle:     b.XRPC.Auth.Handle,
			Did:        b.XRPC.Auth.Did,
		},
		Host:      b.XRPC.Host,
		UserAgent: b.XRPC.UserAgent,
		Headers:   b.XRPC.Headers,
	}
	resp, err := atproto.ServerRefreshSession(ctx, refreshClient)
	if err != nil {
		return err
	}
	// TODO check account status in response
	meta := b.UserLogin.Metadata.(*UserLoginMetadata)
	if resp.Did != meta.Auth.Did {
		return fmt.Errorf("DID changed from %s to %s", meta.Auth.Did, resp.Did)
	}
	meta.Auth.RefreshJwt = resp.RefreshJwt
	meta.Auth.AccessJwt = resp.AccessJwt
	log := zerolog.Ctx(ctx)
	if resp.Handle != meta.Auth.Handle {
		log.Debug().
			Str("old_handle", meta.Auth.Handle).
			Str("new_handle", resp.Handle).
			Msg("Handle changed")
		meta.Auth.Handle = resp.Handle
		b.UserLogin.RemoteName = resp.Handle
		b.UserLogin.RemoteProfile.Username = resp.Handle
	}
	if resp.DidDoc != nil {
		ident, err := parseDIDDoc(resp.DidDoc)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to parse DID doc")
		} else if ident != nil {
			pdsEndpoint := ident.PDSEndpoint()
			if pdsEndpoint != "" && meta.Host != pdsEndpoint {
				log.Debug().
					Str("old_pds", meta.Host).
					Str("new_pds", pdsEndpoint).
					Msg("PDS endpoint changed")
				meta.Host = pdsEndpoint
				b.XRPC.Host = pdsEndpoint
				b.ChatRPC.Host = pdsEndpoint
			}
		}
	}
	err = b.UserLogin.Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to save refreshed login: %w", err)
	}
	return nil
}

const PollInterval = 5 * time.Second

func (b *BlueskyClient) nextAccessTokenExpiry() time.Time {
	parsedJWT, err := jwt.Parse([]byte(b.XRPC.Auth.AccessJwt), jwt.WithVerify(false))
	if err != nil {
		return time.Now().Add(10 * time.Minute)
	}
	return parsedJWT.Expiration()
}

func (b *BlueskyClient) fetchInbox(ctx context.Context) error {
	const limit = 20
	// TODO support paginating list
	chats, err := chat.ConvoListConvos(ctx, b.ChatRPC, "", limit)
	if err != nil {
		return err
	}
	for _, chatInfo := range chats.Convos {
		var latestMessageTS time.Time
		if chatInfo.LastMessage != nil {
			if chatInfo.LastMessage.ConvoDefs_MessageView != nil {
				latestMessageTS, _ = syntax.ParseDatetimeTime(chatInfo.LastMessage.ConvoDefs_MessageView.SentAt)
			} else if chatInfo.LastMessage.ConvoDefs_DeletedMessageView != nil {
				latestMessageTS, _ = syntax.ParseDatetimeTime(chatInfo.LastMessage.ConvoDefs_DeletedMessageView.SentAt)
			}
		}
		b.UserLogin.QueueRemoteEvent(&simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type: bridgev2.RemoteEventChatResync,
				LogContext: func(c zerolog.Context) zerolog.Context {
					return c.Str("chat_id", chatInfo.Id)
				},
				PortalKey:    b.makePortalKey(chatInfo.Id),
				CreatePortal: true,
			},
			ChatInfo:            b.wrapChatInfo(ctx, chatInfo),
			LatestMessageTS:     latestMessageTS,
			BundledBackfillData: chatInfo,
		})
	}
	return nil
}

func (b *BlueskyClient) startPolling() {
	ctx, cancel := context.WithCancel(context.Background())
	oldCancel := b.stopPolling.Swap(&cancel)
	if oldCancel != nil {
		(*oldCancel)()
	}
	log := b.UserLogin.Log.With().Str("action", "bluesky polling").Logger()
	ctx = log.WithContext(ctx)
	log.Info().Time("next_token_expiry", b.nextAccessTokenExpiry()).Msg("Starting polling")
	ticker := time.NewTicker(PollInterval)
	expiryTimer := time.NewTimer(time.Until(b.nextAccessTokenExpiry()) - 2*time.Minute)
	defer func() {
		ticker.Stop()
		log.Debug().Msg("Stopped polling")
	}()
	ctxDone := ctx.Done()
	isErroring := true
	for {
		err := b.pollOnce(ctx)
		if err != nil {
			isErroring = true
			log.Err(err).Msg("Failed to poll for messages")
			b.UserLogin.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateTransientDisconnect,
				Error:      "bsky-poll-failed",
			})
			// TODO sleep with backoff
		} else if isErroring {
			isErroring = false
			b.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
		}
		select {
		case <-ticker.C:
		case <-expiryTimer.C:
			err = b.refreshToken(ctx)
			if err != nil {
				log.Err(err).Msg("Failed to refresh token")
				expiryTimer.Reset(30 * time.Second)
				b.UserLogin.BridgeState.Send(status.BridgeState{
					StateEvent: status.StateUnknownError,
					Error:      "bsky-token-refresh-failed",
				})
			} else {
				nextExpiry := b.nextAccessTokenExpiry()
				log.Debug().Time("next_expiry", nextExpiry).Msg("Refreshed token")
				expiryTimer.Reset(time.Until(nextExpiry) - 2*time.Minute)
				b.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
			}
		case <-ctxDone:
			return
		}
	}
}

func (b *BlueskyClient) pollOnce(ctx context.Context) error {
	log := zerolog.Ctx(ctx)
	meta := b.UserLogin.Metadata.(*UserLoginMetadata)
	resp, err := chat.ConvoGetLog(ctx, b.ChatRPC, meta.Cursor)
	if err != nil {
		return err
	}
	for _, log := range resp.Logs {
		b.HandleEvent(ctx, log)
	}
	if resp.Cursor != nil && *resp.Cursor != meta.Cursor {
		meta.Cursor = *resp.Cursor
		err = b.UserLogin.Save(ctx)
		if err != nil {
			log.Err(err).Msg("Failed to save updated polling cursor")
		}
	}
	return nil
}

func (b *BlueskyClient) Disconnect() {
	stop := b.stopPolling.Swap(nil)
	if stop != nil {
		(*stop)()
	}
}

func (b *BlueskyClient) IsLoggedIn() bool {
	return b.XRPC.Auth != nil
}

func (b *BlueskyClient) LogoutRemote(ctx context.Context) {
	if b.XRPC.Auth != nil {
		err := atproto.ServerDeleteSession(ctx, b.XRPC)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to delete session")
		}
	}
}

func (b *BlueskyClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return b.IsLoggedIn() && string(parseUserID(userID)) == b.XRPC.Auth.Did
}
