// mautrix-bluesky - A Matrix-Bluesky puppeting bridge.
// Copyright (C) 2026 Renfroe Innovation, LLC
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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/api/chat"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
)

var (
	_ bridgev2.DeleteChatHandlingNetworkAPI = (*BlueskyClient)(nil)
	_ bridgev2.UserBlockingNetworkAPI       = (*BlueskyClient)(nil)
)

const (
	blockCollection = "app.bsky.graph.block"
	reasonSpam      = "com.atproto.moderation.defs#reasonSpam"
	// BlueskyModerationProxy routes createReport to Bluesky's own moderation
	// service, as the official app does, rather than relying on the PDS's
	// default report service (which a self-hosted PDS may not have).
	BlueskyModerationProxy = "did:plc:ar7c4by46qjdydhdevvrndac#atproto_labeler"
)

// HandleMatrixDeleteChat leaves the conversation (chat.bsky.convo.leaveConvo),
// which removes it from the user's chat list. Bluesky has no
// delete-for-everyone.
func (b *BlueskyClient) HandleMatrixDeleteChat(ctx context.Context, msg *bridgev2.MatrixDeleteChat) error {
	if msg.Content != nil && msg.Content.DeleteForEveryone {
		return fmt.Errorf("bluesky can't delete a chat for everyone; it can only leave it")
	}
	_, err := chat.ConvoLeaveConvo(ctx, b.ChatRPC, &chat.ConvoLeaveConvo_Input{ConvoId: parsePortalID(msg.Portal.ID)})
	if err != nil {
		return fmt.Errorf("failed to leave the Bluesky conversation: %w", err)
	}
	return nil
}

// HandleMatrixBlockUser blocks or unblocks the other member of a DM, and with
// report_spam also files a spam report against the account. A block is a
// public app.bsky.graph.block record in the user's repo, the same as Block in
// the Bluesky app.
func (b *BlueskyClient) HandleMatrixBlockUser(ctx context.Context, msg *bridgev2.MatrixBlockUser) error {
	if msg.Content.ReportSpam && !msg.Content.Block {
		return fmt.Errorf("can't report spam while unblocking")
	}
	target, err := b.otherMember(ctx, msg.Portal)
	if err != nil {
		return err
	}
	me := b.XRPC.Auth.Did
	profile, err := bsky.ActorGetProfile(ctx, b.XRPC, target)
	if err != nil {
		return fmt.Errorf("failed to read the Bluesky profile: %w", err)
	}
	var existing string
	if profile.Viewer != nil && profile.Viewer.Blocking != nil {
		existing = *profile.Viewer.Blocking
	}
	log := zerolog.Ctx(ctx).With().Str("target_did", target).Logger()
	if msg.Content.Block {
		if existing == "" {
			_, err = atproto.RepoCreateRecord(ctx, b.XRPC, &atproto.RepoCreateRecord_Input{
				Collection: blockCollection,
				Repo:       me,
				Record: &lexutil.LexiconTypeDecoder{Val: &bsky.GraphBlock{
					LexiconTypeID: blockCollection,
					CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
					Subject:       target,
				}},
			})
			if err != nil {
				return fmt.Errorf("failed to block on Bluesky: %w", err)
			}
			log.Info().Msg("Blocked user on Bluesky")
		} else {
			log.Debug().Str("block_uri", existing).Msg("User already blocked on Bluesky")
		}
		if msg.Content.ReportSpam {
			if err = b.reportSpam(ctx, target); err != nil {
				return err
			}
			log.Info().Msg("Reported user as spam on Bluesky")
		}
		return nil
	}
	if existing == "" {
		return nil
	}
	rkey, err := blockRkey(existing, me)
	if err != nil {
		return err
	}
	_, err = atproto.RepoDeleteRecord(ctx, b.XRPC, &atproto.RepoDeleteRecord_Input{
		Collection: blockCollection,
		Repo:       me,
		Rkey:       rkey,
	})
	if err != nil {
		return fmt.Errorf("failed to unblock on Bluesky: %w", err)
	}
	log.Info().Msg("Unblocked user on Bluesky")
	return nil
}

func (b *BlueskyClient) reportSpam(ctx context.Context, target string) error {
	modRPC := &xrpc.Client{
		Client:    b.XRPC.Client,
		Host:      b.XRPC.Host,
		Auth:      b.XRPC.Auth,
		UserAgent: b.XRPC.UserAgent,
		Headers:   map[string]string{"Atproto-Proxy": BlueskyModerationProxy},
	}
	reason := reasonSpam
	_, err := atproto.ModerationCreateReport(ctx, modRPC, &atproto.ModerationCreateReport_Input{
		ReasonType: &reason,
		Subject:    &atproto.ModerationCreateReport_Input_Subject{AdminDefs_RepoRef: &atproto.AdminDefs_RepoRef{Did: target}},
	})
	if err != nil {
		return fmt.Errorf("blocked, but failed to report spam on Bluesky: %w", err)
	}
	return nil
}

// otherMember returns the DID of the other member of a DM conversation.
func (b *BlueskyClient) otherMember(ctx context.Context, portal *bridgev2.Portal) (string, error) {
	resp, err := chat.ConvoGetConvo(ctx, b.ChatRPC, parsePortalID(portal.ID))
	if err != nil {
		return "", fmt.Errorf("failed to read the Bluesky conversation: %w", err)
	}
	me := b.XRPC.Auth.Did
	var others []string
	for _, m := range resp.Convo.Members {
		if m != nil && m.Did != me {
			others = append(others, m.Did)
		}
	}
	if len(others) != 1 {
		return "", fmt.Errorf("conversation has %d other members, can only block in a 1:1 chat", len(others))
	}
	return others[0], nil
}

// blockRkey extracts the record key from at://<me>/app.bsky.graph.block/<rkey>.
func blockRkey(uri, me string) (string, error) {
	prefix := "at://" + me + "/" + blockCollection + "/"
	if !strings.HasPrefix(uri, prefix) || len(uri) == len(prefix) {
		return "", errors.New("unexpected block record URI " + uri)
	}
	return uri[len(prefix):], nil
}
