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
	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"
)

func (b *BlueskyClient) HandleEvent(ctx context.Context, evt *logElemWithReply) {
	zerolog.Ctx(ctx).Trace().Any("evt", evt).Msg("Received event")
	switch {
	case evt.ConvoDefs_LogCreateMessage != nil:
		b.HandleNewMessage(ctx, evt.ConvoDefs_LogCreateMessage, evt.ReplyToID)
	case evt.ConvoDefs_LogAddReaction != nil:
		logEvt := evt.ConvoDefs_LogAddReaction
		b.HandleReaction(ctx, logEvt.ConvoId, logEvt.Rev, reactionTargetMessageID(logEvt.Message), logEvt.Reaction, false)
	case evt.ConvoDefs_LogRemoveReaction != nil:
		logEvt := evt.ConvoDefs_LogRemoveReaction
		b.HandleReaction(ctx, logEvt.ConvoId, logEvt.Rev, removedReactionTargetMessageID(logEvt.Message), logEvt.Reaction, true)
	case evt.ConvoDefs_LogEditGroup != nil:
		b.HandleGroupEdit(ctx, evt.ConvoDefs_LogEditGroup)
	case evt.ConvoDefs_LogAddMember != nil:
		b.HandleMemberChange(ctx, evt.ConvoDefs_LogAddMember.ConvoId, systemMessageMember(evt.ConvoDefs_LogAddMember.Message), event.MembershipJoin)
	case evt.ConvoDefs_LogMemberJoin != nil:
		b.HandleMemberChange(ctx, evt.ConvoDefs_LogMemberJoin.ConvoId, systemMessageMember(evt.ConvoDefs_LogMemberJoin.Message), event.MembershipJoin)
	case evt.ConvoDefs_LogRemoveMember != nil:
		b.HandleMemberChange(ctx, evt.ConvoDefs_LogRemoveMember.ConvoId, systemMessageMember(evt.ConvoDefs_LogRemoveMember.Message), event.MembershipLeave)
	case evt.ConvoDefs_LogMemberLeave != nil:
		b.HandleMemberChange(ctx, evt.ConvoDefs_LogMemberLeave.ConvoId, systemMessageMember(evt.ConvoDefs_LogMemberLeave.Message), event.MembershipLeave)
	default:
	}
}

func (b *BlueskyClient) HandleGroupEdit(ctx context.Context, evt *chat.ConvoDefs_LogEditGroup) {
	var newName *string
	if evt.Message != nil && evt.Message.Data != nil && evt.Message.Data.ConvoDefs_SystemMessageDataEditGroup != nil {
		newName = evt.Message.Data.ConvoDefs_SystemMessageDataEditGroup.NewName
	}
	if newName == nil {
		// The edit wasn't a name change (or has an unknown shape), refetch the full info instead.
		b.queueChatResync(ctx, evt.ConvoId)
		return
	}
	b.UserLogin.QueueRemoteEvent(&simplevent.ChatInfoChange{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventChatInfoChange,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("chat_id", evt.ConvoId).Str("rev", evt.Rev)
			},
			PortalKey: b.makePortalKey(evt.ConvoId),
		},
		ChatInfoChange: &bridgev2.ChatInfoChange{
			ChatInfo: &bridgev2.ChatInfo{Name: newName},
		},
	})
}

func systemMessageMember(msg *chat.ConvoDefs_SystemMessageView) *chat.ConvoDefs_SystemMessageReferredUser {
	if msg == nil || msg.Data == nil {
		return nil
	}
	switch data := msg.Data; {
	case data.ConvoDefs_SystemMessageDataAddMember != nil:
		return data.ConvoDefs_SystemMessageDataAddMember.Member
	case data.ConvoDefs_SystemMessageDataMemberJoin != nil:
		return data.ConvoDefs_SystemMessageDataMemberJoin.Member
	case data.ConvoDefs_SystemMessageDataRemoveMember != nil:
		return data.ConvoDefs_SystemMessageDataRemoveMember.Member
	case data.ConvoDefs_SystemMessageDataMemberLeave != nil:
		return data.ConvoDefs_SystemMessageDataMemberLeave.Member
	default:
		return nil
	}
}

func (b *BlueskyClient) HandleMemberChange(ctx context.Context, convoID string, member *chat.ConvoDefs_SystemMessageReferredUser, membership event.Membership) {
	if member == nil {
		// The event didn't identify the affected member, refetch the full info instead.
		b.queueChatResync(ctx, convoID)
		return
	}
	sender, err := b.makeEventSender(member.Did)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Str("chat_id", convoID).Msg("Failed to parse member DID")
		return
	}
	b.UserLogin.QueueRemoteEvent(&simplevent.ChatInfoChange{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventChatInfoChange,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("chat_id", convoID).Str("member_id", string(sender.Sender))
			},
			PortalKey: b.makePortalKey(convoID),
		},
		ChatInfoChange: &bridgev2.ChatInfoChange{
			MemberChanges: &bridgev2.ChatMemberList{
				MemberMap: map[networkid.UserID]bridgev2.ChatMember{
					sender.Sender: {
						EventSender: sender,
						Membership:  membership,
					},
				},
			},
		},
	})
}

func (b *BlueskyClient) queueChatResync(ctx context.Context, convoID string) {
	if convoID == "" {
		zerolog.Ctx(ctx).Warn().Msg("Dropping chat update event without convo ID")
		return
	}
	b.UserLogin.QueueRemoteEvent(&simplevent.ChatResync{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventChatResync,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("chat_id", convoID)
			},
			PortalKey: b.makePortalKey(convoID),
		},
		GetChatInfoFunc: b.GetChatInfo,
	})
}

func reactionTargetMessageID(msg *chat.ConvoDefs_LogAddReaction_Message) string {
	if msg == nil {
		return ""
	}
	return messageUnionID(msg.ConvoDefs_MessageView, msg.ConvoDefs_DeletedMessageView)
}

func removedReactionTargetMessageID(msg *chat.ConvoDefs_LogRemoveReaction_Message) string {
	if msg == nil {
		return ""
	}
	return messageUnionID(msg.ConvoDefs_MessageView, msg.ConvoDefs_DeletedMessageView)
}

func messageUnionID(msgView *chat.ConvoDefs_MessageView, deletedMsgView *chat.ConvoDefs_DeletedMessageView) string {
	if msgView != nil {
		return msgView.Id
	} else if deletedMsgView != nil {
		return deletedMsgView.Id
	}
	return ""
}

// convertReactionView converts a reaction view into its bridged parts, returning ok=false (with logging) if the view is unusable.
func (b *BlueskyClient) convertReactionView(ctx context.Context, reaction *chat.ConvoDefs_ReactionView) (sender bridgev2.EventSender, emoji string, createdAt time.Time, ok bool) {
	if reaction == nil || reaction.Sender == nil {
		zerolog.Ctx(ctx).Warn().Msg("Dropping reaction with missing data")
		return
	}
	sender, err := b.makeEventSender(reaction.Sender.Did)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to parse reaction sender DID")
		return
	}
	emoji = variationselector.FullyQualify(reaction.Value)
	createdAt, _ = syntax.ParseDatetimeTime(reaction.CreatedAt)
	ok = true
	return
}

func (b *BlueskyClient) HandleReaction(ctx context.Context, convoID, rev, msgID string, reaction *chat.ConvoDefs_ReactionView, remove bool) {
	if msgID == "" {
		zerolog.Ctx(ctx).Warn().Str("chat_id", convoID).Str("rev", rev).Msg("Dropping reaction event with missing target message")
		return
	}
	sender, emoji, createdAt, ok := b.convertReactionView(ctx, reaction)
	if !ok {
		return
	}
	meta := simplevent.EventMeta{
		Type: bridgev2.RemoteEventReaction,
		LogContext: func(c zerolog.Context) zerolog.Context {
			return c.
				Str("chat_id", convoID).
				Str("rev", rev).
				Str("message_id", msgID).
				Str("sender_id", string(sender.Sender))
		},
		PortalKey: b.makePortalKey(convoID),
		Sender:    sender,
	}
	if remove {
		meta.Type = bridgev2.RemoteEventReactionRemove
	} else {
		meta.Timestamp = createdAt
	}
	b.UserLogin.QueueRemoteEvent(&simplevent.Reaction{
		EventMeta:      meta,
		TargetMessage:  makeMessageID(makePortalID(convoID), msgID),
		EmojiID:        networkid.EmojiID(emoji),
		Emoji:          emoji,
		ReactionDBMeta: &ReactionMetadata{Value: reaction.Value},
	})
}

func (b *BlueskyClient) HandleNewMessage(ctx context.Context, evt *chat.ConvoDefs_LogCreateMessage, replyToID string) {
	if evt.Message == nil {
		zerolog.Ctx(ctx).Warn().Str("chat_id", evt.ConvoId).Str("rev", evt.Rev).Msg("Dropping message event without message")
		return
	}
	sender, sentAt, msgID, msgData, err := b.parseMessageDetails(evt.Message.ConvoDefs_MessageView, evt.Message.ConvoDefs_DeletedMessageView)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to parse message details")
		return
	}
	msgData = wrapMessageData(msgData, replyToID)
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

// messageWithReply pairs a message view with the ID of the message it replies to, which the indigo structs don't carry.
type messageWithReply struct {
	*chat.ConvoDefs_MessageView
	ReplyToID string
}

func wrapMessageData(msgData any, replyToID string) any {
	if msgView, ok := msgData.(*chat.ConvoDefs_MessageView); ok && replyToID != "" {
		return &messageWithReply{ConvoDefs_MessageView: msgView, ReplyToID: replyToID}
	}
	return msgData
}

func convertMessageView(msgView *chat.ConvoDefs_MessageView) *bridgev2.ConvertedMessage {
	return &bridgev2.ConvertedMessage{
		Parts: []*bridgev2.ConvertedMessagePart{{
			Type: event.EventMessage,
			Content: &event.MessageEventContent{
				MsgType: event.MsgText,
				Body:    msgView.Text,
			},
		}},
	}
}

func convertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, data any) (*bridgev2.ConvertedMessage, error) {
	switch typedData := any(data).(type) {
	case *chat.ConvoDefs_MessageView:
		return convertMessageView(typedData), nil
	case *messageWithReply:
		converted := convertMessageView(typedData.ConvoDefs_MessageView)
		converted.ReplyTo = &networkid.MessageOptionalPartID{
			MessageID: makeMessageID(portal.ID, typedData.ReplyToID),
		}
		return converted, nil
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
