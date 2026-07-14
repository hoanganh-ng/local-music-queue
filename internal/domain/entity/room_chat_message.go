package entity

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxChatContentLen is the R11a documented maximum content length for a
// room chat message, counted in Unicode code points (post-trim, post
// CRLF/CR normalization). The handler rejects longer submissions with
// ErrChatContentTooLong before persistence.
//
// The cap is enforced via utf8.RuneCountInString rather than len()
// because byte length is not a stable cross-language cap: a single
// emoji or CJK code point is 3-4 bytes in UTF-8 but counts as one
// "character" to the user. The frontend mirrors this rule with
// Array.from(content).length so a UI limit of 500 matches the
// authoritative server limit.
const MaxChatContentLen = 500

// RoomChatMessage represents a single plain-text message persisted in a
// room's chat. Content is stored exactly as accepted (after the
// trim/CRLF normalization performed by NormalizeChatContent) — no HTML
// escape is performed at the persistence layer.
type RoomChatMessage struct {
	ID        int64     `json:"id"`
	RoomID    int64     `json:"room_id"`
	SenderID  int       `json:"sender_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrEmptyChatContent is returned when chat content is empty after the
// trim/CRLF normalization performed by NormalizeChatContent.
var ErrEmptyChatContent = errors.New("empty chat content")

// ErrChatContentTooLong is returned when trimmed content exceeds
// MaxChatContentLen Unicode code points.
var ErrChatContentTooLong = errors.New("chat content too long")

// NormalizeChatContent applies the R11a content rules: trim leading and
// trailing whitespace (Unicode-aware), normalize CRLF and CR line
// endings to LF, and reject empty or oversized content. The returned
// string is the canonical form that the repository persists.
//
// The trim uses strings.TrimSpace so leading/trailing tabs, NBSPs,
// zero-width spaces, and other Unicode whitespace are stripped along
// with the standard ASCII whitespace classes.
//
// The length cap is enforced in Unicode code points via
// utf8.RuneCountInString AFTER the trim and CRLF normalization so the
// wire cap matches what the user actually typed. The byte length
// (len(s)) can exceed 500 for emoji-heavy content even when the user
// "typed" 10 characters; rune count keeps the cap stable.
func NormalizeChatContent(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ErrEmptyChatContent
	}
	// Normalize CRLF/CR to LF (server-side, before the length check).
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if utf8.RuneCountInString(s) > MaxChatContentLen {
		return "", ErrChatContentTooLong
	}
	return s, nil
}