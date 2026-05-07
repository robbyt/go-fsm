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

// Option configures subscriber behavior.
type Option func(*Config)

// Config holds configuration for subscriber channels.
type Config struct {
	channel         chan string
	externalChannel bool
	timeout         time.Duration
}

// WithBufferSize creates a new internal channel with the specified buffer size.
// A negative size is treated as 0 (unbuffered) to avoid a panic in make.
// If a previous option set an external channel via WithCustomChannel, this
// option overrides it: the channel will be owned by the manager and closed
// when the context is cancelled.
func WithBufferSize(size int) Option {
	bufSize := size
	if bufSize < 0 {
		bufSize = 0
	}
	return func(config *Config) {
		config.channel = make(chan string, bufSize)
		config.externalChannel = false
	}
}

// WithCustomChannel uses an external channel instead of creating a new one.
func WithCustomChannel(ch chan string) Option {
	return func(config *Config) {
		config.channel = ch
		config.externalChannel = true
	}
}

// WithTimeout sets a timeout for broadcast delivery.
// - timeout = 0 (default): best-effort, drops message if channel is full
// - timeout > 0: blocks up to timeout duration, then drops
// - timeout < 0: blocks indefinitely until delivered (guaranteed delivery)
func WithTimeout(timeout time.Duration) Option {
	return func(config *Config) {
		config.timeout = timeout
	}
}
