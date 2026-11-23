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
	"go.mau.fi/util/l10n"
)

var L = l10n.NewNamespace("mautrix-bluesky")

var (
	msgFlowPasswordName         = L.Define("login.flow.password.name", "Username & password")
	msgFlowPasswordDescription  = L.Define("login.flow.password.description", "Log in by entering your Bluesky username and password")
	msgFieldDomainName          = L.Define("login.field.domain.name", "Server")
	msgFieldDomainDescription   = L.Define("login.field.domain.description", "Bluesky server. The default server is `bsky.social`")
	msgFieldUsernameName        = L.Define("login.field.username.name", "Username or email")
	msgFieldUsernameDescription = L.Define("login.field.username.description", "Bluesky account handle or email address")
	msgFieldPasswordName        = L.Define("login.field.password.name", "Password")
	msgFieldPasswordDescription = L.Define("login.field.password.description", "Bluesky account password")
	msgStepCompleteInstructions = L.Define(
		"login.step.complete.instructions", "Successfully logged in as {handle}",
		l10n.WithComment("handle is the Bluesky handle of the logged-in account, e.g. alice.bsky.social"),
	)
)
