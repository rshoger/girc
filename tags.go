// Copyright 2016 Liam Stanley <me@liamstanley.io>. All rights reserved.
// Use of this source code is governed by the MIT license that can be
// found in the LICENSE file.

package girc

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

const (
	prefixTag      byte = 0x40 // @
	prefixTagValue byte = 0x3D // =
	tagSeparator   byte = 0x3B // ;
	maxTagLength   int  = 511  // 510 + @ and " " (space), though space usually not included.
)

// Tags represents the key-value pairs in IRCv3 message tags. The map contains
// the encoded message-tag values. If the tag is present, it may still be
// empty. See Tags.Get() and Tags.Set() for use with getting/setting
// information within the tags.
//
// Note that retrieving and setting tags are not concurrent safe. If this is
// necessary, you will need to implement it yourself.
type Tags map[string]string

// ParseTags parses out the key-value map of tags. raw should only be the tag
// data, not a full message. For example:
//   @aaa=bbb;ccc;example.com/ddd=eee
// NOT:
//   @aaa=bbb;ccc;example.com/ddd=eee :nick!ident@host.com PRIVMSG me :Hello
func ParseTags(raw string) (t Tags) {
	t = make(Tags)
	parts := strings.Split(raw, string(tagSeparator))
	var hasValue int

	for i := 0; i < len(parts); i++ {
		hasValue = strings.IndexByte(parts[i], prefixTagValue)

		if hasValue < 1 {
			// The tag doesn't contain a value.
			t[parts[i]] = ""
			continue
		}

		// May have equals sign and no value as well.
		if len(parts[i]) < hasValue+1 {
			t[parts[i]] = ""
			continue
		}

		t[parts[i][:hasValue]] = parts[i][hasValue+1:]
		continue
	}

	return t
}

// Len determines the length of the string representation of this tag map.
func (t Tags) Len() (length int) {
	return len(t.String())
}

// Count finds how many total tags that there are.
func (t Tags) Count() int {
	return len(t)
}

// Bytes returns a []byte representation of this tag map.
func (t Tags) Bytes() []byte {
	max := len(t)
	if max == 0 {
		return nil
	}

	buffer := new(bytes.Buffer)
	var current int

	for tagName, tagValue := range t {
		// Trim at max allowed chars.
		if (buffer.Len() + len(tagName) + len(tagValue) + 2) > maxTagLength {
			return buffer.Bytes()
		}

		buffer.WriteString(tagName)

		// Write the value as necessary.
		if len(tagValue) > 0 {
			buffer.WriteByte(prefixTagValue)
			buffer.WriteString(tagValue)
		}

		// add the separator ";" between tags.
		if current <= max {
			buffer.WriteByte(tagSeparator)
		}

		current++
	}

	return buffer.Bytes()
}

// String returns a string representation of this tag map.
func (t Tags) String() string {
	return string(t.Bytes())
}

func (t Tags) writeTo(w io.Writer) (n int, err error) {
	b := t.Bytes()
	if len(b) == 0 {
		return n, err
	}

	n, err = w.Write(b)
	if err != nil {
		return n, err
	}

	var j int
	j, err = w.Write([]byte{eventSpace})
	n += j

	return n, err
}

// tagDecode are encoded -> decoded pairs for replacement to decode.
var tagDecode = []string{
	"\\:", ";",
	"\\s", " ",
	"\\\\", "\\",
	"\\r", "\r",
	"\\n", "\n",
}
var tagDecoder = strings.NewReplacer(tagDecode...)

// tagEncode are decoded -> encoded pairs for replacement to decode.
var tagEncode = []string{
	";", "\\:",
	" ", "\\s",
	"\\", "\\\\",
	"\r", "\\r",
	"\n", "\\n",
}
var tagEncoder = strings.NewReplacer(tagEncode...)

// Get returns the unescaped value of given tag key. Note that this is not
// concurrent safe.
func (t Tags) Get(key string) (tag string, success bool) {
	if _, ok := t[key]; ok {
		tag = tagDecoder.Replace(t[key])
		success = true
	}

	return tag, success
}

// Set escapes given value and saves it as the value for given key. Note that
// this is not concurrent safe.
func (t Tags) Set(key, value string) error {
	if !validTag(key) {
		return fmt.Errorf("tag %q is invalid", key)
	}

	value = tagEncoder.Replace(value)

	// Check to make sure it's not too long here.
	if (t.Len() + len(key) + len(value) + 2) > maxTagLength {
		return fmt.Errorf("unable to set tag %q [value %q]: tags too long for message", key, value)
	}

	t[key] = value

	return nil
}

// Remove deletes the tag frwom the tag map.
func (t Tags) Remove(key string) (success bool) {
	if _, success = t[key]; success {
		delete(t, key)
	}

	return success
}

func validTag(name string) bool {
	if len(name) < 1 {
		return false
	}

	for i := 0; i < len(name); i++ {
		// A-Z, a-z, 0-9, -/._
		if (name[i] < 0x41 || name[i] > 0x5A) && (name[i] < 0x61 || name[i] > 0x7A) && (name[i] < 0x2D || name[i] > 0x39) && name[i] != 0x5F {
			return false
		}
	}

	return true
}