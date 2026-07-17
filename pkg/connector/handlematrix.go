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

	"github.com/bluesky-social/indigo/api/chat"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/rs/zerolog"
	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

var (
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*BlueskyClient)(nil)
	_ bridgev2.ReactionHandlingNetworkAPI    = (*BlueskyClient)(nil)
)

func (b *BlueskyClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (message *bridgev2.MatrixMessageResponse, err error) {
	if !msg.Content.MsgType.IsText() {
		return nil, fmt.Errorf("%w %s", bridgev2.ErrUnsupportedMessageType, msg.Content.MsgType)
	}
	resp, err := chat.ConvoSendMessage(ctx, b.ChatRPC, &chat.ConvoSendMessage_Input{
		ConvoId: parsePortalID(msg.Portal.ID),
		Message: &chat.ConvoDefs_MessageInput{
			Text: msg.Content.Body,
		},
	})
	if err != nil {
		return nil, err
	}
	sentAt, err := syntax.ParseDatetimeTime(resp.SentAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sentAt: %w", err)
	}
	senderID, err := makeUserIDFromString(resp.Sender.Did)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sender DID: %w", err)
	}
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        makeMessageID(msg.Portal.ID, resp.Id),
			SenderID:  senderID,
			Timestamp: sentAt,
		},
		StreamOrder: sentAt.UnixMilli(),
	}, nil
}

// maxReactionsPerMessage is the ReactionLimitReached threshold enforced by chat.bsky.convo.addReaction.
const maxReactionsPerMessage = 5

func (b *BlueskyClient) PreHandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	senderID, err := makeUserIDFromString(parseUserLoginID(b.UserLogin.ID))
	if err != nil {
		return bridgev2.MatrixReactionPreResponse{}, fmt.Errorf("failed to parse own DID: %w", err)
	}
	emoji := variationselector.FullyQualify(msg.Content.RelatesTo.Key)
	return bridgev2.MatrixReactionPreResponse{
		SenderID:     senderID,
		EmojiID:      networkid.EmojiID(emoji),
		Emoji:        emoji,
		MaxReactions: maxReactionsPerMessage,
	}, nil
}

func (b *BlueskyClient) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (*database.Reaction, error) {
	_, msgID := parseMessageID(msg.TargetMessage.ID)
	if msgID == "" {
		return nil, fmt.Errorf("failed to parse target message ID")
	}
	_, err := chat.ConvoAddReaction(ctx, b.ChatRPC, &chat.ConvoAddReaction_Input{
		ConvoId:   parsePortalID(msg.Portal.ID),
		MessageId: msgID,
		Value:     msg.PreHandleResp.Emoji,
	})
	if err != nil {
		return nil, err
	}
	return &database.Reaction{}, nil
}

func (b *BlueskyClient) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	_, msgID := parseMessageID(msg.TargetReaction.MessageID)
	if msgID == "" {
		return fmt.Errorf("failed to parse target message ID")
	}
	_, err := chat.ConvoRemoveReaction(ctx, b.ChatRPC, &chat.ConvoRemoveReaction_Input{
		ConvoId:   parsePortalID(msg.Portal.ID),
		MessageId: msgID,
		Value:     string(msg.TargetReaction.EmojiID),
	})
	return err
}

func (b *BlueskyClient) HandleMatrixReadReceipt(ctx context.Context, msg *bridgev2.MatrixReadReceipt) error {
	var msgID *string
	if msg.ExactMessage != nil {
		_, msgIDVal := parseMessageID(msg.ExactMessage.ID)
		if msgIDVal != "" {
			msgID = &msgIDVal
		}
	}
	resp, err := chat.ConvoUpdateRead(ctx, b.ChatRPC, &chat.ConvoUpdateRead_Input{
		ConvoId:   parsePortalID(msg.Portal.ID),
		MessageId: msgID,
	})
	zerolog.Ctx(ctx).Trace().Any("resp", resp).Err(err).Msg("Read receipt bridged")
	return err
}
