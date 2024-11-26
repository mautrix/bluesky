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
)

type BlueskyConnector struct {
	Bridge *bridgev2.Bridge
	Config Config
}

var _ bridgev2.NetworkConnector = (*BlueskyConnector)(nil)

func (b *BlueskyConnector) GetName() bridgev2.BridgeName {
	return bridgev2.BridgeName{
		DisplayName:          "Bluesky",
		NetworkURL:           "https://bsky.app",
		NetworkIcon:          "mxc://maunium.net/ezAjjDxhiJWGEohmhkpfeHYf",
		NetworkID:            "bluesky",
		BeeperBridgeType:     "bluesky",
		DefaultPort:          29340,
		DefaultCommandPrefix: "!bsky",
	}
}

func (b *BlueskyConnector) Init(bridge *bridgev2.Bridge) {
	b.Bridge = bridge
}

func (b *BlueskyConnector) Start(ctx context.Context) error {
	return nil
}
