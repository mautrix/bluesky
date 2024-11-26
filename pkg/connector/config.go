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
	_ "embed"
	"strings"
	"text/template"

	up "go.mau.fi/util/configupgrade"
	"gopkg.in/yaml.v3"
)

type Config struct {
	DisplaynameTemplate string `yaml:"displayname_template"`

	displaynameTemplate *template.Template `yaml:"-"`
}

//go:embed example-config.yaml
var ExampleConfig string

type umConfig Config

func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	err := node.Decode((*umConfig)(c))
	if err != nil {
		return err
	}
	return c.PostProcess()
}

func (c *Config) PostProcess() error {
	var err error
	c.displaynameTemplate, err = template.New("displayname").Parse(c.DisplaynameTemplate)
	return err
}

type DisplaynameParams struct {
	DisplayName string
	Handle      string
	DID         string
}

func (c *Config) FormatDisplayname(displayName, handle, did string) string {
	var nameBuf strings.Builder
	err := c.displaynameTemplate.Execute(&nameBuf, DisplaynameParams{
		DisplayName: displayName,
		Handle:      handle,
		DID:         did,
	})
	if err != nil {
		panic(err)
	}
	return nameBuf.String()
}

func upgradeConfig(helper up.Helper) {
	helper.Copy(up.Str, "displayname_template")
}

func (b *BlueskyConnector) GetConfig() (example string, data any, upgrader up.Upgrader) {
	return ExampleConfig, &b.Config, &up.StructUpgrader{
		SimpleUpgrader: up.SimpleUpgrader(upgradeConfig),
		Blocks:         [][]string{},
		Base:           ExampleConfig,
	}
}
