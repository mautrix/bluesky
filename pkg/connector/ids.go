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
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/whyrusleeping/go-did"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func makeUserLoginID(did string) networkid.UserLoginID {
	return networkid.UserLoginID(did)
}

func parseUserLoginID(id networkid.UserLoginID) string {
	return string(id)
}

func makeUserIDFromString(rawDID string) (networkid.UserID, error) {
	parsedDID, err := did.ParseDID(rawDID)
	if err != nil {
		return "", err
	}
	return makeUserID(parsedDID), nil
}

func makeUserID(parsedDID did.DID) networkid.UserID {
	return networkid.UserID(fmt.Sprintf("%s-%s", parsedDID.Protocol(), parsedDID.Value()))
}

//func makeUserID2(parsedDID syntax.DID) networkid.UserID {
//	return networkid.UserID(fmt.Sprintf("%s-%s", parsedDID.Method(), parsedDID.Identifier()))
//}

func parseUserID(id networkid.UserID) syntax.DID {
	parts := strings.SplitN(string(id), "-", 2)
	if len(parts) != 2 {
		return ""
	}
	return syntax.DID(fmt.Sprintf("did:%s:%s", parts[0], parts[1]))
}

func makePortalID(id string) networkid.PortalID {
	return networkid.PortalID(id)
}

func parsePortalID(id networkid.PortalID) string {
	return string(id)
}

func makeMessageID(chatID networkid.PortalID, msgID string) networkid.MessageID {
	return networkid.MessageID(fmt.Sprintf("%s:%s", chatID, msgID))
}

func parseMessageID(id networkid.MessageID) (networkid.PortalID, string) {
	parts := strings.SplitN(string(id), ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return networkid.PortalID(parts[0]), parts[1]
}

func (b *BlueskyClient) makeEventSender(userDID string) (bridgev2.EventSender, error) {
	userID, err := makeUserIDFromString(userDID)
	if err != nil {
		return bridgev2.EventSender{}, err
	}
	return bridgev2.EventSender{
		IsFromMe:    userDID == parseUserLoginID(b.UserLogin.ID),
		SenderLogin: makeUserLoginID(userDID),
		Sender:      userID,
	}, nil
}

func (b *BlueskyClient) makePortalKey(chatID string) networkid.PortalKey {
	return networkid.PortalKey{
		ID: makePortalID(chatID),
		// Bluesky only supports DMs currently, so always use a receiver
		Receiver: b.UserLogin.ID,
	}
}
