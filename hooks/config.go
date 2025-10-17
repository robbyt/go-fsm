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

package hooks

// HookType represents the type of hook (pre or post transition).
type HookType int

const (
	HookTypePre HookType = iota
	HookTypePost
)

// String returns the string representation of HookType.
func (h HookType) String() string {
	switch h {
	case HookTypePre:
		return "pre"
	case HookTypePost:
		return "post"
	default:
		return "unknown"
	}
}

// PreTransitionHookConfig contains configuration for registering a pre-transition hook.
type PreTransitionHookConfig struct {
	Name  string
	From  []string
	To    []string
	Guard GuardFunc
}

// PostTransitionHookConfig contains configuration for registering a post-transition hook.
type PostTransitionHookConfig struct {
	Name   string
	From   []string
	To     []string
	Action ActionFunc
}

// HookInfo represents information about a registered hook, returned by GetHooks.
type HookInfo struct {
	Name       string
	FromStates []string
	ToStates   []string
	Type       HookType
}
