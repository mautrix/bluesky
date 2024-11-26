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
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
)

var (
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*BlueskyClient)(nil)
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
