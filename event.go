// Copyright (c) Liam Stanley <me@liamstanley.io>. All rights reserved. Use
// of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package girc

import (
	"bytes"
	"fmt"
	"strings"
)

const (
	eventSpace byte = 0x20 // Separator.
	maxLength       = 510  // Maximum length is 510 (2 for line endings).
)

// cutCRFunc is used to trim CR characters from prefixes/messages.
func cutCRFunc(r rune) bool {
	return r == '\r' || r == '\n'
}

// Event represents an IRC protocol message, see RFC1459 section 2.3.1
//
//    <message>  :: [':' <prefix> <SPACE>] <command> <params> <crlf>
//    <prefix>   :: <servername> | <nick> ['!' <user>] ['@' <host>]
//    <command>  :: <letter>{<letter>} | <number> <number> <number>
//    <SPACE>    :: ' '{' '}
//    <params>   :: <SPACE> [':' <trailing> | <middle> <params>]
//    <middle>   :: <Any *non-empty* sequence of octets not including SPACE or NUL
//                   or CR or LF, the first of which may not be ':'>
//    <trailing> :: <Any, possibly empty, sequence of octets not including NUL or
//                   CR or LF>
//    <crlf>     :: CR LF
type Event struct {
	Source        *Source  // The source of the event.
	Tags          Tags     // IRCv3 style message tags. Only use if network supported.
	Command       string   // the IRC command, e.g. JOIN, PRIVMSG, KILL.
	Params        []string // parameters to the command. Commonly nickname, channel, etc.
	Trailing      string   // any trailing data. e.g. with a PRIVMSG, this is the message text.
	EmptyTrailing bool     // if true, trailing prefix (:) will be added even if Event.Trailing is empty.
	Sensitive     bool     // if the message is sensitive (e.g. and should not be logged).
}

// ParseEvent takes a string and attempts to create a Event struct.
//
// Returns nil if the Event is invalid.
func ParseEvent(raw string) (e *Event) {
	// Ignore empty events.
	if raw = strings.TrimFunc(raw, cutCRFunc); len(raw) < 2 {
		return nil
	}

	i, j := 0, 0
	e = &Event{}

	if raw[0] == prefixTag {
		// Tags end with a space.
		i = strings.IndexByte(raw, eventSpace)

		if i < 2 {
			return nil
		}

		e.Tags = ParseTags(raw[1:i])
		raw = raw[i+1:]
	}

	if raw[0] == messagePrefix {
		// Prefix ends with a space.
		i = strings.IndexByte(raw, eventSpace)

		// Prefix string must not be empty if the indicator is present.
		if i < 2 {
			return nil
		}

		e.Source = ParseSource(raw[1:i])

		// Skip space at the end of the prefix.
		i++
	}

	// Find end of command.
	j = i + strings.IndexByte(raw[i:], eventSpace)

	// Extract command.
	if j < i {
		e.Command = strings.ToUpper(raw[i:])
		return e
	}

	e.Command = strings.ToUpper(raw[i:j])
	// Skip space after command.
	j++

	// Find prefix for trailer.
	i = strings.IndexByte(raw[j:], messagePrefix)

	if i < 0 || raw[j+i-1] != eventSpace {
		// No trailing argument.
		e.Params = strings.Split(raw[j:], string(eventSpace))
		return e
	}

	// Compensate for index on substring.
	i = i + j

	// Check if we need to parse arguments.
	if i > j {
		e.Params = strings.Split(raw[j:i-1], string(eventSpace))
	}

	e.Trailing = raw[i+1:]

	// We need to re-encode the trailing argument even if it was empty.
	if len(e.Trailing) <= 0 {
		e.EmptyTrailing = true
	}

	return e
}

// Copy makes a deep copy of a given event, for use with allowing untrusted
// functions/handlers edit the event without causing potential issues with
// other handlers.
func (e *Event) Copy() *Event {
	newEvent := &Event{}

	*newEvent = *e

	// Copy Source field, as it's a pointer and needs to be dereferenced.
	if e.Source != nil {
		*newEvent.Source = *e.Source
	}

	// Copy Params in order to dereference as well.
	if e.Params != nil {
		copy(newEvent.Params, e.Params)
	}

	// Copy tags as necessary.
	if e.Tags != nil {
		newEvent.Tags = Tags{}
		for k, v := range e.Tags {
			newEvent.Tags[k] = v
		}
	}

	return newEvent
}

// Len calculates the length of the string representation of event.
func (e *Event) Len() (length int) {
	if e.Tags != nil {
		// Include tags and trailing space.
		length = e.Tags.Len() + 1
	}
	if e.Source != nil {
		// Include prefix and trailing space.
		length += e.Source.Len() + 2
	}

	length += len(e.Command)

	if len(e.Params) > 0 {
		length += len(e.Params)

		for i := 0; i < len(e.Params); i++ {
			length += len(e.Params[i])
		}
	}

	if len(e.Trailing) > 0 || e.EmptyTrailing {
		// Include prefix and space.
		length += len(e.Trailing) + 2
	}

	return
}

// Bytes returns a []byte representation of event. Strips all newlines and
// carriage returns.
//
// Per RFC2812 section 2.3, messages should not exceed 512 characters in
// length. This method forces that limit by discarding any characters
// exceeding the length limit.
func (e *Event) Bytes() []byte {
	buffer := new(bytes.Buffer)

	// Tags.
	if e.Tags != nil {
		e.Tags.writeTo(buffer)
	}

	// Event prefix.
	if e.Source != nil {
		buffer.WriteByte(messagePrefix)
		e.Source.writeTo(buffer)
		buffer.WriteByte(eventSpace)
	}

	// Command is required.
	buffer.WriteString(e.Command)

	// Space separated list of arguments.
	if len(e.Params) > 0 {
		buffer.WriteByte(eventSpace)
		buffer.WriteString(strings.Join(e.Params, string(eventSpace)))
	}

	if len(e.Trailing) > 0 || e.EmptyTrailing {
		buffer.WriteByte(eventSpace)
		buffer.WriteByte(messagePrefix)
		buffer.WriteString(e.Trailing)
	}

	// We need the limit the buffer length.
	if buffer.Len() > (maxLength) {
		if e.Tags != nil {
			// regular message, max tag length, and the splitting space.
			buffer.Truncate(maxLength + maxTagLength + 1)
		} else {
			buffer.Truncate(maxLength)
		}
	}

	out := buffer.Bytes()

	// Strip newlines and carriage returns.
	for i := 0; i < len(out); i++ {
		if out[i] == 0x0A || out[i] == 0x0D {
			out = append(out[:i], out[i+1:]...)
			i-- // Decrease the index so we can pick up where we left off.
		}
	}

	return out
}

// String returns a string representation of this event. Strips all newlines
// and carriage returns.
func (e *Event) String() string {
	return string(e.Bytes())
}

// Pretty returns a prettified string of the event. If the event doesn't
// support prettification, ok is false. Pretty is not just useful to make
// an event prettier, but also to filter out events that most don't visually
// see in normal IRC clients. e.g. most clients don't show WHO queries.
func (e *Event) Pretty() (out string, ok bool) {
	if e.Sensitive {
		return "", false
	}

	if e.Source == nil {
		if e.Command != PRIVMSG && e.Command != NOTICE {
			return "", false
		}

		if len(e.Params) > 0 && len(e.Trailing) > 0 {
			return fmt.Sprintf("[>] writing %s [%s]: %s", strings.ToLower(e.Command), strings.Join(e.Params, ", "), e.Trailing), true
		} else if len(e.Params) > 0 {
			return fmt.Sprintf("[>] writing %s [%s]", strings.ToLower(e.Command), strings.Join(e.Params, ", ")), true
		} else if len(e.Trailing) > 0 {
			return fmt.Sprintf("[>] writing %s: %s", strings.ToLower(e.Command), e.Trailing), true
		}

		return "", false
	}

	if e.Command == INITIALIZED {
		return fmt.Sprintf("[*] connection to %s initialized", e.Trailing), true
	}

	if e.Command == CONNECTED {
		return fmt.Sprintf("[*] successfully connected to %s", e.Trailing), true
	}

	if (e.Command == PRIVMSG || e.Command == NOTICE) && len(e.Params) > 0 {
		if ctcp := decodeCTCP(e); ctcp != nil {
			if ctcp.Reply {
				return
			}

			return fmt.Sprintf("[*] CTCP query from %s: %s%s", ctcp.Source.Name, ctcp.Command, " "+ctcp.Text), true
		}
		return fmt.Sprintf("[%s] (%s) %s", strings.Join(e.Params, ","), e.Source.Name, e.Trailing), true
	}

	if e.Command == RPL_MOTD || e.Command == RPL_MOTDSTART ||
		e.Command == RPL_WELCOME || e.Command == RPL_YOURHOST ||
		e.Command == RPL_CREATED || e.Command == RPL_LUSERCLIENT {
		return "[*] " + e.Trailing, true
	}

	if e.Command == JOIN && len(e.Params) > 0 {
		return fmt.Sprintf("[*] %s (%s) has joined %s", e.Source.Name, e.Source.Host, e.Params[0]), true
	}

	if e.Command == PART && len(e.Params) > 0 {
		return fmt.Sprintf("[*] %s (%s) has left %s (%s)", e.Source.Name, e.Source.Host, e.Params[0], e.Trailing), true
	}

	if e.Command == ERROR {
		return fmt.Sprintf("[*] an error occurred: %s", e.Trailing), true
	}

	if e.Command == QUIT {
		return fmt.Sprintf("[*] %s has quit (%s)", e.Source.Name, e.Trailing), true
	}

	if e.Command == KICK && len(e.Params) == 2 {
		return fmt.Sprintf("[%s] *** %s has kicked %s: %s", e.Params[0], e.Source.Name, e.Params[1], e.Trailing), true
	}

	if e.Command == NICK && len(e.Params) == 1 {
		return fmt.Sprintf("[*] %s is now known as %s", e.Source.Name, e.Params[0]), true
	}

	if e.Command == TOPIC && len(e.Params) > 0 {
		return fmt.Sprintf("[%s] *** %s has set the topic to: %s", e.Params[len(e.Params)-1], e.Source.Name, e.Trailing), true
	}

	if e.Command == MODE && len(e.Params) > 2 {
		return fmt.Sprintf("[%s] *** %s set modes: %s", e.Params[0], e.Source.Name, strings.Join(e.Params[1:], " ")), true
	}

	if e.Command == CAP_AWAY {
		if len(e.Trailing) > 0 {
			return fmt.Sprintf("[*] %s is now away: %s", e.Source.Name, e.Trailing), true
		}

		return fmt.Sprintf("[*] %s is no longer away", e.Source.Name), true
	}

	if e.Command == CAP_CHGHOST && len(e.Params) == 2 {
		return fmt.Sprintf("[*] %s has changed their host to %s (was %s)", e.Source.Name, e.Params[1], e.Source.Host), true
	}

	if e.Command == CAP_ACCOUNT && len(e.Params) == 1 {
		if e.Params[0] == "*" {
			return fmt.Sprintf("[*] %s has become un-authenticated", e.Source.Name), true
		}

		return fmt.Sprintf("[*] %s has authenticated for account: %s", e.Source.Name, e.Params[0]), true
	}

	if e.Command == RPL_TOPIC && len(e.Params) > 0 && len(e.Trailing) > 0 {
		return fmt.Sprintf("[*] topic for %s is: %s", e.Params[len(e.Params)-1], e.Trailing), true
	}

	return "", false
}

// IsAction checks to see if the event is a PRIVMSG, and is an ACTION (/me).
func (e *Event) IsAction() bool {
	if len(e.Trailing) <= 0 || e.Command != PRIVMSG {
		return false
	}

	if !strings.HasPrefix(e.Trailing, "\001ACTION") || e.Trailing[len(e.Trailing)-1] != ctcpDelim {
		return false
	}

	return true
}

// IsFromChannel checks to see if a message was from a channel (rather than
// a private message).
func (e *Event) IsFromChannel() bool {
	if len(e.Params) != 1 {
		return false
	}

	if e.Command != "PRIVMSG" || !IsValidChannel(e.Params[0]) {
		return false
	}

	return true
}

// IsFromUser checks to see if a message was from a user (rather than a
// channel).
func (e *Event) IsFromUser() bool {
	if len(e.Params) != 1 {
		return false
	}

	if e.Command != "PRIVMSG" || !IsValidNick(e.Params[0]) {
		return false
	}

	return true
}

// StripAction returns the stripped version of the action encoding from a
// PRIVMSG ACTION (/me).
func (e *Event) StripAction() string {
	if !e.IsAction() || len(e.Trailing) < 9 {
		return e.Trailing
	}

	return e.Trailing[8 : len(e.Trailing)-1]
}

const (
	messagePrefix byte = 0x3A // ":" -- prefix or last argument
	prefixIdent   byte = 0x21 // "!" -- username
	prefixHost    byte = 0x40 // "@" -- hostname
)

// Source represents the sender of an IRC event, see RFC1459 section 2.3.1.
// <servername> | <nick> [ '!' <user> ] [ '@' <host> ]
type Source struct {
	// Name is the nickname, server name, or service name.
	Name string
	// Ident is commonly known as the "user".
	Ident string
	// Host is the hostname or IP address of the user/service. Is not accurate
	// due to how IRC servers can spoof hostnames.
	Host string
}

// ParseSource takes a string and attempts to create a Source struct.
func ParseSource(raw string) (src *Source) {
	src = new(Source)

	user := strings.IndexByte(raw, prefixIdent)
	host := strings.IndexByte(raw, prefixHost)

	switch {
	case user > 0 && host > user:
		src.Name = raw[:user]
		src.Ident = raw[user+1 : host]
		src.Host = raw[host+1:]
	case user > 0:
		src.Name = raw[:user]
		src.Ident = raw[user+1:]
	case host > 0:
		src.Name = raw[:host]
		src.Host = raw[host+1:]
	default:
		src.Name = raw
	}

	return src
}

// Len calculates the length of the string representation of prefix
func (s *Source) Len() (length int) {
	length = len(s.Name)
	if len(s.Ident) > 0 {
		length = 1 + length + len(s.Ident)
	}
	if len(s.Host) > 0 {
		length = 1 + length + len(s.Host)
	}

	return
}

// Bytes returns a []byte representation of source.
func (s *Source) Bytes() []byte {
	buffer := new(bytes.Buffer)
	s.writeTo(buffer)

	return buffer.Bytes()
}

// String returns a string representation of source.
func (s *Source) String() (out string) {
	out = s.Name
	if len(s.Ident) > 0 {
		out = out + string(prefixIdent) + s.Ident
	}
	if len(s.Host) > 0 {
		out = out + string(prefixHost) + s.Host
	}

	return
}

// IsHostmask returns true if source looks like a user hostmask.
func (s *Source) IsHostmask() bool {
	return len(s.Ident) > 0 && len(s.Host) > 0
}

// IsServer returns true if this source looks like a server name.
func (s *Source) IsServer() bool {
	return len(s.Ident) <= 0 && len(s.Host) <= 0
}

// writeTo is an utility function to write the source to the bytes.Buffer
// in Event.String().
func (s *Source) writeTo(buffer *bytes.Buffer) {
	buffer.WriteString(s.Name)
	if len(s.Ident) > 0 {
		buffer.WriteByte(prefixIdent)
		buffer.WriteString(s.Ident)
	}
	if len(s.Host) > 0 {
		buffer.WriteByte(prefixHost)
		buffer.WriteString(s.Host)
	}

	return
}
