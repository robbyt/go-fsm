/*
Copyright 2024 Robert Terhaar <robbyt@robbyt.net>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package broadcast

import "time"

const (
	defaultAsyncTimeout = 0
	defaultSyncTimeout  = 10 * time.Second
)

// Option configures subscriber behavior.
type Option func(*Config)

// Config holds configuration for subscriber channels.
type Config struct {
	customChannel chan string
	syncTimeout   time.Duration
}

// WithBufferSize creates a channel with the specified buffer size.
func WithBufferSize(size int) Option {
	return func(config *Config) {
		config.customChannel = make(chan string, size)
	}
}

// WithCustomChannel uses an external channel instead of creating a new one.
func WithCustomChannel(ch chan string) Option {
	return func(config *Config) {
		config.customChannel = ch
	}
}

// WithSyncBroadcast enables synchronous broadcasting that blocks until the message
// is delivered to the channel, rather than dropping messages for full channels.
func WithSyncBroadcast() Option {
	return func(config *Config) {
		config.syncTimeout = defaultSyncTimeout
	}
}

// WithSyncTimeout sets the timeout for synchronous broadcast operations.
func WithSyncTimeout(timeout time.Duration) Option {
	return func(config *Config) {
		config.syncTimeout = timeout
	}
}
