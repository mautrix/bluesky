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
	"slices"

	"github.com/bluesky-social/indigo/api/chat"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/rs/zerolog"
	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

var _ bridgev2.BackfillingNetworkAPI = (*BlueskyClient)(nil)

func (b *BlueskyClient) FetchMessages(ctx context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	if !params.Forward {
		return nil, fmt.Errorf("backward backfill is not yet supported")
	}
	resp, err := convoGetMessagesWithReply(ctx, b.ChatRPC, parsePortalID(params.Portal.ID), "", min(int64(params.Count), 100))
	if err != nil {
		return nil, err
	}
	slices.Reverse(resp.Messages)
	convertedMessages := make([]*bridgev2.BackfillMessage, 0, len(resp.Messages))
	for _, msg := range resp.Messages {
		sender, sentAt, msgID, msgData, err := b.parseMessageDetails(msg.ConvoDefs_MessageView, msg.ConvoDefs_DeletedMessageView)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to parse message details")
			continue
		} else if params.AnchorMessage != nil && !sentAt.After(params.AnchorMessage.Timestamp) {
			continue
		}
		msgData = wrapMessageData(msgData, msg.ReplyToID)
		data, err := convertMessage(ctx, params.Portal, params.Portal.Bridge.Bot, msgData)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to convert message")
			continue
		}
		convertedMessages = append(convertedMessages, &bridgev2.BackfillMessage{
			ConvertedMessage: data,
			Sender:           sender,
			ID:               makeMessageID(params.Portal.ID, msgID),
			Timestamp:        sentAt,
			StreamOrder:      sentAt.UnixMilli(),
			Reactions:        b.convertBackfillReactions(ctx, msg.ConvoDefs_MessageView),
		})
	}
	chatInfo, ok := params.BundledData.(*chat.ConvoDefs_ConvoView)
	return &bridgev2.FetchMessagesResponse{
		Messages: convertedMessages,
		Forward:  true,
		MarkRead: ok && chatInfo != nil && chatInfo.UnreadCount == 0,
	}, nil
}

func (b *BlueskyClient) convertBackfillReactions(ctx context.Context, msgView *chat.ConvoDefs_MessageView) []*bridgev2.BackfillReaction {
	if msgView == nil || len(msgView.Reactions) == 0 {
		return nil
	}
	reactions := make([]*bridgev2.BackfillReaction, 0, len(msgView.Reactions))
	for _, reaction := range msgView.Reactions {
		if reaction == nil || reaction.Sender == nil {
			continue
		}
		sender, err := b.makeEventSender(reaction.Sender.Did)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to parse reaction sender DID")
			continue
		}
		emoji := variationselector.FullyQualify(reaction.Value)
		backfillReaction := &bridgev2.BackfillReaction{
			Sender:  sender,
			EmojiID: networkid.EmojiID(emoji),
			Emoji:   emoji,
		}
		if createdAt, err := syntax.ParseDatetimeTime(reaction.CreatedAt); err == nil {
			backfillReaction.Timestamp = createdAt
		}
		reactions = append(reactions, backfillReaction)
	}
	return reactions
}
