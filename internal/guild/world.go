package guild

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	WorldSpeakerLevel = LevelUnlock
	WorldHistoryLimit = 100
)

type WorldChat struct {
	Messages      []Message `json:"messages"`
	NextMessageID int64     `json:"next_message_id"`
}

func (w WorldChat) clone() WorldChat {
	w.Messages = slices.Clone(w.Messages)
	return w
}

func (w *WorldChat) LastMessageID() int64 {
	return lastMessageID(w.Messages)
}

func (w *WorldChat) MessagesAfter(id int64) []Message {
	return messagesAfter(w.Messages, id)
}

func (w *WorldChat) post(message Message) {
	w.NextMessageID++
	message.ID = w.NextMessageID
	w.Messages = appendWithinLimit(w.Messages, message, WorldHistoryLimit)
}

func lastMessageID(messages []Message) int64 {
	if len(messages) == 0 {
		return 0
	}
	return messages[len(messages)-1].ID
}

func messagesAfter(messages []Message, id int64) []Message {
	start := slices.IndexFunc(messages, func(m Message) bool { return m.ID > id })
	if start < 0 {
		return []Message{}
	}
	return slices.Clone(messages[start:])
}

func appendWithinLimit(messages []Message, message Message, limit int) []Message {
	messages = append(messages, message)
	if extra := len(messages) - limit; extra > 0 {
		messages = slices.Clone(messages[extra:])
	}
	return messages
}

func chatLine(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", refuse(InvalidArgument, "message is empty")
	}
	if utf8.RuneCountInString(text) > MaxChatRunes {
		return "", refuse(ChatMessageTooLong, "message is longer than %d characters", MaxChatRunes)
	}
	if hasMarkup(text) {
		return "", refuse(InvalidArgument, "message cannot contain < or >")
	}
	return text, nil
}

func (s *State) SayToWorld(p Player, text string, now int64) error {
	if p.Level < WorldSpeakerLevel {
		return refuse(ChatLevelTooLow, "world chat opens for writing at level %d", WorldSpeakerLevel)
	}
	line, err := chatLine(text)
	if err != nil {
		return err
	}
	message := Message{Time: now, Text: line, PlayerID: p.ID, PlayerName: p.Name, Level: p.Level, Rank: RankNotMember}
	if g := s.GuildOf(p.ID); g != nil {
		member, _ := g.Member(p.ID)
		message.Rank = member.Rank
		message.GuildName = g.Name
	}
	s.World.post(message)
	return nil
}
