package api

import (
	"net/http"

	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
)

type worldMessagesBody struct {
	After int64  `json:"after"`
	Text  string `json:"text"`
}

type worldMessagesReply struct {
	Messages      []guild.Message `json:"messages"`
	LastMessageID int64           `json:"last_message_id"`
}

func worldReply(state *guild.State, after int64) worldMessagesReply {
	return worldMessagesReply{Messages: state.World.MessagesAfter(after), LastMessageID: state.World.LastMessageID()}
}

func (s *Server) worldMessages(w http.ResponseWriter, r *http.Request, player string) {
	var request worldMessagesBody
	if !readGuildBody(w, r, &request) {
		return
	}
	if request.Text == "" {
		var reply worldMessagesReply
		s.guilds.View(func(state *guild.State) { reply = worldReply(state, request.After) })
		writeJSON(w, http.StatusOK, reply)
		return
	}
	if !s.allow(w, s.limiters.worldChatByPlayer, player) {
		return
	}
	p, ok := s.guildPlayer(w, player)
	if !ok {
		return
	}
	var reply worldMessagesReply
	err := s.guilds.Update(func(state *guild.State) error {
		if err := state.SayToWorld(p, request.Text, s.now().Unix()); err != nil {
			return err
		}
		reply = worldReply(state, request.After)
		return nil
	})
	if err != nil {
		s.writeGuildFailure(w, player, err)
		return
	}
	s.log.Info("world chat", "player", player, "name", p.Name, "text", request.Text)
	writeJSON(w, http.StatusOK, reply)
}
