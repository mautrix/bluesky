/*
mautrix-bluesky - A Matrix-Bluesky puppeting bridge.
Copyright (C) 2024 Tulir Asokan

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package connector

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/indigo/api/chat"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

// This file re-implements chat calls with lexicon fields missing from the pinned indigo version (replyTo, group convo info); delete once the SDK catches up.

type replyRef struct {
	MessageId string `json:"messageId"`
}

type messageInputWithReply struct {
	chat.ConvoDefs_MessageInput
	ReplyTo *replyRef `json:"replyTo,omitempty"`
}

type sendMessageInputWithReply struct {
	ConvoId string                 `json:"convoId"`
	Message *messageInputWithReply `json:"message"`
}

func convoSendMessageWithReply(ctx context.Context, c lexutil.LexClient, input *sendMessageInputWithReply) (*chat.ConvoDefs_MessageView, error) {
	var out chat.ConvoDefs_MessageView
	if err := c.LexDo(ctx, lexutil.Procedure, "application/json", "chat.bsky.convo.sendMessage", nil, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// replyToExtract pulls the ID of the replied-to message out of an embedded messageView, ignoring the rest of the view.
type replyToExtract struct {
	ReplyTo struct {
		Id string `json:"id"`
	} `json:"replyTo"`
}

type logElemWithReply struct {
	chat.ConvoGetLog_Output_Logs_Elem
	ReplyToID string `json:"-"`
	// RawType and RawConvoID are always set, allowing log types unknown to the pinned indigo version to be recognized.
	RawType    string `json:"-"`
	RawConvoID string `json:"-"`
}

func (e *logElemWithReply) UnmarshalJSON(b []byte) error {
	if err := e.ConvoGetLog_Output_Logs_Elem.UnmarshalJSON(b); err != nil {
		return err
	}
	var extract struct {
		Type    string         `json:"$type"`
		ConvoID string         `json:"convoId"`
		Message replyToExtract `json:"message"`
	}
	if err := json.Unmarshal(b, &extract); err == nil {
		e.ReplyToID = extract.Message.ReplyTo.Id
		e.RawType = extract.Type
		e.RawConvoID = extract.ConvoID
	}
	return nil
}

type getLogOutputWithReply struct {
	Cursor *string             `json:"cursor,omitempty"`
	Logs   []*logElemWithReply `json:"logs"`
}

func convoGetLogWithReply(ctx context.Context, c lexutil.LexClient, cursor string) (*getLogOutputWithReply, error) {
	var out getLogOutputWithReply
	params := map[string]interface{}{}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if err := c.LexDo(ctx, lexutil.Query, "", "chat.bsky.convo.getLog", params, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

const groupConvoType = "chat.bsky.convo.defs#groupConvo"

const logEditGroupType = "chat.bsky.convo.defs#logEditGroup"

// convoViewWithKind adds the convoView kind union, which carries the name of group conversations.
type convoViewWithKind struct {
	chat.ConvoDefs_ConvoView
	IsGroup   bool   `json:"-"`
	GroupName string `json:"-"`
}

func (c *convoViewWithKind) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &c.ConvoDefs_ConvoView); err != nil {
		return err
	}
	var extract struct {
		Kind struct {
			Type string `json:"$type"`
			Name string `json:"name"`
		} `json:"kind"`
	}
	if err := json.Unmarshal(b, &extract); err == nil {
		c.IsGroup = extract.Kind.Type == groupConvoType
		c.GroupName = extract.Kind.Name
	}
	return nil
}

type getConvoOutputWithKind struct {
	Convo *convoViewWithKind `json:"convo"`
}

func convoGetConvoWithKind(ctx context.Context, c lexutil.LexClient, convoID string) (*getConvoOutputWithKind, error) {
	var out getConvoOutputWithKind
	params := map[string]interface{}{
		"convoId": convoID,
	}
	if err := c.LexDo(ctx, lexutil.Query, "", "chat.bsky.convo.getConvo", params, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type listConvosOutputWithKind struct {
	Convos []*convoViewWithKind `json:"convos"`
	Cursor *string              `json:"cursor,omitempty"`
}

func convoListConvosWithKind(ctx context.Context, c lexutil.LexClient, cursor string, limit int64) (*listConvosOutputWithKind, error) {
	var out listConvosOutputWithKind
	params := map[string]interface{}{}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if limit != 0 {
		params["limit"] = limit
	}
	if err := c.LexDo(ctx, lexutil.Query, "", "chat.bsky.convo.listConvos", params, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type messageElemWithReply struct {
	chat.ConvoGetMessages_Output_Messages_Elem
	ReplyToID string `json:"-"`
}

func (e *messageElemWithReply) UnmarshalJSON(b []byte) error {
	if err := e.ConvoGetMessages_Output_Messages_Elem.UnmarshalJSON(b); err != nil {
		return err
	}
	var extract replyToExtract
	if err := json.Unmarshal(b, &extract); err == nil {
		e.ReplyToID = extract.ReplyTo.Id
	}
	return nil
}

type getMessagesOutputWithReply struct {
	Cursor   *string                 `json:"cursor,omitempty"`
	Messages []*messageElemWithReply `json:"messages"`
}

func convoGetMessagesWithReply(ctx context.Context, c lexutil.LexClient, convoID, cursor string, limit int64) (*getMessagesOutputWithReply, error) {
	var out getMessagesOutputWithReply
	params := map[string]interface{}{
		"convoId": convoID,
	}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if limit != 0 {
		params["limit"] = limit
	}
	if err := c.LexDo(ctx, lexutil.Query, "", "chat.bsky.convo.getMessages", params, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
