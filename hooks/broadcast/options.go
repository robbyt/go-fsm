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

// WithBufferSize creates a channel with the specified buffer size.
func WithBufferSize(size int) Option {
	return func(config *Config) {
		config.channel = make(chan string, size)
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
// Positive duration: blocks up to the specified duration before dropping the message.
// Negative duration: blocks indefinitely until delivered (guaranteed delivery).
// Zero duration (default): best-effort, drops immediately if channel is full.
func WithTimeout(timeout time.Duration) Option {
	return func(config *Config) {
		config.timeout = timeout
	}
}
