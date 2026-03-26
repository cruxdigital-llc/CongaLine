package channels

import (
	"fmt"
	"sort"
	"strings"
)

var registered = map[string]Channel{}

// Register adds a channel implementation. Panics on duplicate.
func Register(ch Channel) {
	name := ch.Name()
	if _, exists := registered[name]; exists {
		panic(fmt.Sprintf("channels: duplicate registration %q", name))
	}
	registered[name] = ch
}

// Get returns the channel for the given platform name.
func Get(name string) (Channel, bool) {
	ch, ok := registered[name]
	return ch, ok
}

// All returns all registered channels.
func All() map[string]Channel {
	return registered
}

// ParseBinding parses "platform:id" into a ChannelBinding.
func ParseBinding(s string) (ChannelBinding, error) {
	i := strings.Index(s, ":")
	if i < 0 {
		return ChannelBinding{}, fmt.Errorf("invalid channel binding %q: expected format platform:id (e.g., slack:U0123456789)", s)
	}
	platform := s[:i]
	id := s[i+1:]
	if _, ok := registered[platform]; !ok {
		return ChannelBinding{}, fmt.Errorf("unknown channel platform %q; registered: %v", platform, registeredNames())
	}
	return ChannelBinding{Platform: platform, ID: id}, nil
}

func registeredNames() []string {
	names := make([]string, 0, len(registered))
	for n := range registered {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
