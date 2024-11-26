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
	"time"

	"github.com/bluesky-social/indigo/api/chat"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

func (b *BlueskyClient) HandleEvent(ctx context.Context, evt *chat.ConvoGetLog_Output_Logs_Elem) {
	zerolog.Ctx(ctx).Trace().Any("evt", evt).Msg("Received event")
	switch {
	case evt.ConvoDefs_LogCreateMessage != nil:
		b.HandleNewMessage(ctx, evt.ConvoDefs_LogCreateMessage)
	default:
	}
}

func (b *BlueskyClient) HandleNewMessage(ctx context.Context, evt *chat.ConvoDefs_LogCreateMessage) {
	sender, sentAt, msgID, msgData, err := b.parseMessageDetails(evt.Message.ConvoDefs_MessageView, evt.Message.ConvoDefs_DeletedMessageView)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to parse message details")
		return
	}
	b.UserLogin.QueueRemoteEvent(&simplevent.Message[any]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessage,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.
					Str("chat_id", evt.ConvoId).
					Str("rev", evt.Rev).
					Str("message_id", msgID).
					Str("sender_id", string(sender.Sender))
			},
			PortalKey:    b.makePortalKey(evt.ConvoId),
			Sender:       sender,
			CreatePortal: true,
			Timestamp:    sentAt,
			StreamOrder:  sentAt.UnixMilli(),
		},
		Data:               msgData,
		ID:                 makeMessageID(makePortalID(evt.ConvoId), msgID),
		ConvertMessageFunc: convertMessage,
	})
}

func (b *BlueskyClient) parseMessageDetails(
	msgView *chat.ConvoDefs_MessageView, deletedMsgView *chat.ConvoDefs_DeletedMessageView,
) (evtSender bridgev2.EventSender, sentAt time.Time, msgID string, msgData any, err error) {
	var senderDID, sentAtStr string
	if msgView != nil {
		senderDID = msgView.Sender.Did
		sentAtStr = msgView.SentAt
		msgID = msgView.Id
		msgData = msgView
	} else if deletedMsgView != nil {
		senderDID = deletedMsgView.Sender.Did
		sentAtStr = deletedMsgView.SentAt
		msgID = deletedMsgView.Id
		msgData = deletedMsgView
	} else {
		err = fmt.Errorf("no message view or deleted message view")
		return
	}
	evtSender, err = b.makeEventSender(senderDID)
	if err != nil {
		err = fmt.Errorf("failed to parse sender DID: %w", err)
		return
	}
	sentAt, err = syntax.ParseDatetimeTime(sentAtStr)
	if err != nil {
		err = fmt.Errorf("failed to parse sentAt: %w", err)
		return
	}
	return
}

func convertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, data any) (*bridgev2.ConvertedMessage, error) {
	switch typedData := any(data).(type) {
	case *chat.ConvoDefs_MessageView:
		return &bridgev2.ConvertedMessage{
			Parts: []*bridgev2.ConvertedMessagePart{{
				Type: event.EventMessage,
				Content: &event.MessageEventContent{
					MsgType: event.MsgText,
					Body:    typedData.Text,
				},
			}},
		}, nil
	case *chat.ConvoDefs_DeletedMessageView:
		return &bridgev2.ConvertedMessage{
			Parts: []*bridgev2.ConvertedMessagePart{{
				Type: event.EventMessage,
				Content: &event.MessageEventContent{
					MsgType: event.MsgNotice,
					Body:    "Deleted message",
				},
			}},
		}, nil
	default:
		return &bridgev2.ConvertedMessage{
			Parts: []*bridgev2.ConvertedMessagePart{{
				Type: event.EventMessage,
				Content: &event.MessageEventContent{
					MsgType: event.MsgNotice,
					Body:    "Unsupported message",
				},
			}},
		}, nil
	}
}
