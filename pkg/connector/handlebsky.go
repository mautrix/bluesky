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

	"github.com/bluesky-social/indigo/api/chat"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

func (b *BlueskyClient) HandleEvent(evt *chat.ConvoGetLog_Output_Logs_Elem) {
	switch {
	case evt.ConvoDefs_LogCreateMessage != nil:
		b.HandleNewMessage(evt.ConvoDefs_LogCreateMessage)
	}
}

func (b *BlueskyClient) HandleNewMessage(evt *chat.ConvoDefs_LogCreateMessage) {
	msg := evt.Message.ConvoDefs_MessageView
	if msg == nil {
		// TODO bridge placeholders for deleted messages?
		return
	}
	evtSender, err := b.makeEventSender(msg.Sender.Did)
	if err != nil {
		// TODO log
		return
	}
	sentAt, err := syntax.ParseDatetimeTime(msg.SentAt)
	if err != nil {
		// TODO log
		return
	}
	b.UserLogin.QueueRemoteEvent(&simplevent.Message[*chat.ConvoDefs_MessageView]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessage,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.
					Str("chat_id", evt.ConvoId).
					Str("rev", evt.Rev).
					Str("message_id", msg.Id).
					Str("sender_did", msg.Sender.Did)
			},
			PortalKey:    b.makePortalKey(evt.ConvoId),
			Sender:       evtSender,
			CreatePortal: true,
			Timestamp:    sentAt,
			StreamOrder:  sentAt.UnixMilli(),
		},
		Data:               msg,
		ID:                 makeMessageID(makePortalID(evt.ConvoId), msg.Id),
		ConvertMessageFunc: b.convertMessage,
	})
}

func (b *BlueskyClient) convertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, data *chat.ConvoDefs_MessageView) (*bridgev2.ConvertedMessage, error) {
	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type: event.EventMessage,
			Content: &event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    data.Text,
			},
		}},
	}, nil
}
