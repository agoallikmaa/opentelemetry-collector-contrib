// Copyright 2019, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudfoundryreceiver

import (
	"fmt"
	"net/url"

	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/confighttp"
)

type RLPGatewayConfig struct {
	confighttp.HTTPClientSettings `mapstructure:",squash"`
	ShardID                       string `mapstructure:"shard_id"`
}

type UAAConfig struct {
	confighttp.HTTPClientSettings `mapstructure:",squash"`
	Username                      string `mapstructure:"username"`
	Password                      string `mapstructure:"password"`
}

// Config defines configuration for Collectd receiver.
type Config struct {
	config.ReceiverSettings `mapstructure:",squash"`
	RLPGateway              RLPGatewayConfig `mapstructure:"rlp_gateway"`
	UAA                     UAAConfig        `mapstructure:"uaa"`
}

func (c *Config) Validate() error {
	err := validateURLOption("rlp_gateway.endpoint", c.RLPGateway.Endpoint)
	if err != nil {
		return err
	}

	err = validateURLOption("uaa.endpoint", c.RLPGateway.Endpoint)
	if err != nil {
		return err
	}

	if c.UAA.Username == "" {
		return fmt.Errorf("UAA username not specified")
	}

	return nil
}

func validateURLOption(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s not specified", name)
	}

	_, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("failed to parse %s as url: %v", name, err)
	}

	return nil
}
