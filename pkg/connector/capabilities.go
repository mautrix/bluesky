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

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

var generalCaps = &bridgev2.NetworkGeneralCapabilities{
	DisappearingMessages: false,
	AggressiveUpdateInfo: false,
}

func (b *BlueskyConnector) GetCapabilities() *bridgev2.NetworkGeneralCapabilities {
	return generalCaps
}

// Relay patch 0001: capabilities 2 = the features below. bridgev2 re-sends
// com.beeper.room_features to every portal at startup only when this number
// moves; fetchInbox resyncs just the newest 20 convos.
func (b *BlueskyConnector) GetBridgeInfoVersion() (info, capabilities int) {
	return 1, 2
}

// Relay patch 0001: delete chat (leaveConvo) everywhere, and in DMs block_user
// (an app.bsky.graph.block record) + report_spam (createReport reasonSpam).
// The IDs changed with the contents so clients drop the cached features.
var roomCaps = &event.RoomFeatures{
	ID:            "fi.mau.bluesky.capabilities.2026_10_30",
	MaxTextLength: 10000,
	DeleteChat:    true,
}

var dmCaps = &event.RoomFeatures{
	ID:            "fi.mau.bluesky.capabilities.2026_10_30+dm",
	MaxTextLength: 10000,
	DeleteChat:    true,
	BlockUser:     true,
	ReportSpam:    true,
}

func (b *BlueskyClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	if portal != nil && portal.RoomType == database.RoomTypeDM {
		return dmCaps
	}
	return roomCaps
}
