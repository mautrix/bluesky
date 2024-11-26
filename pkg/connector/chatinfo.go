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
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/api/chat"
	"github.com/rs/zerolog"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

func (b *BlueskyClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	chatInfo, err := chat.ConvoGetConvo(ctx, b.ChatRPC, parsePortalID(portal.ID))
	if err != nil {
		return nil, err
	}
	return b.wrapChatInfo(ctx, chatInfo.Convo), nil
}

func (b *BlueskyClient) wrapChatInfo(ctx context.Context, chatInfo *chat.ConvoDefs_ConvoView) *bridgev2.ChatInfo {
	info := &bridgev2.ChatInfo{
		Members: &bridgev2.ChatMemberList{
			IsFull:           true,
			TotalMemberCount: len(chatInfo.Members),
			MemberMap:        make(map[networkid.UserID]bridgev2.ChatMember, len(chatInfo.Members)),
		},
		UserLocal:   &bridgev2.UserLocalPortalInfo{},
		CanBackfill: true,
	}
	if chatInfo.Muted {
		info.UserLocal.MutedUntil = &event.MutedForever
	}
	if len(chatInfo.Members) == 2 {
		info.Type = ptr.Ptr(database.RoomTypeDM)
	}
	for _, member := range chatInfo.Members {
		evtSender, err := b.makeEventSender(member.Did)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Str("member_did", member.Did).Msg("Failed to parse member DID")
			info.Members.IsFull = false
			continue
		}
		info.Members.MemberMap[evtSender.Sender] = bridgev2.ChatMember{
			EventSender: evtSender,
			UserInfo: &bridgev2.UserInfo{
				Identifiers: []string{member.Did, fmt.Sprintf("bluesky:%s", member.Handle)},
				Name:        ptr.Ptr(b.Main.Config.FormatDisplayname(ptr.Val(member.DisplayName), member.Handle, member.Did)),
				Avatar:      b.wrapAvatar(ptr.Val(member.Avatar)),
			},
		}
	}
	return info
}

func (b *BlueskyClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	actorID := string(parseUserID(ghost.ID))
	if actorID == "" {
		return nil, fmt.Errorf("failed to parse ghost ID")
	}
	profile, err := bsky.ActorGetProfile(ctx, b.XRPC, actorID)
	if err != nil {
		return nil, err
	}
	return &bridgev2.UserInfo{
		Identifiers: []string{profile.Did, fmt.Sprintf("bluesky:%s", profile.Handle)},
		Name:        ptr.Ptr(b.Main.Config.FormatDisplayname(ptr.Val(profile.DisplayName), profile.Handle, profile.Did)),
		Avatar:      b.wrapAvatar(ptr.Val(profile.Avatar)),
	}, nil
}

func (b *BlueskyClient) wrapAvatar(url string) *bridgev2.Avatar {
	return &bridgev2.Avatar{
		ID: networkid.AvatarID(url),
		Get: func(ctx context.Context) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to prepare request: %w", err)
			}
			req.Header.Set("User-Agent", *b.XRPC.UserAgent)
			resp, err := b.XRPC.Client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("failed to send request: %w", err)
			}
			data, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("failed to read response: %w", err)
			}
			return data, nil
		},
		Remove: url == "",
	}
}
