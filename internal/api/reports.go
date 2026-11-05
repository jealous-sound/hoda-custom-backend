package api

import (
	"fmt"
	"net/http"

	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/report"
)

const (
	ReportGuild       = "guild"
	ReportApplication = "application"
)

type reportBody struct {
	Kind        string           `json:"kind"`
	AbuseType   report.AbuseType `json:"abuse_type"`
	GuildID     string           `json:"guild_id"`
	ApplicantID string           `json:"applicant_id"`
}

type reportReply struct {
	Status string `json:"status"`
}

func describeGuild(state *guild.State, guildID string) (string, error) {
	g := state.Guild(guildID)
	if g == nil {
		return "", refusalFor(guild.NotFound, fmt.Sprintf("no guild %q", guildID))
	}
	return fmt.Sprintf("Guild Name: %s, Id: %s", g.Name, g.ID), nil
}

func describeApplication(state *guild.State, reporter, applicantID string) (string, error) {
	mine := state.GuildOf(reporter)
	if mine == nil {
		return "", refusalFor(guild.NotInGuild, "not in a guild")
	}
	if member, _ := mine.Member(reporter); member.Rank < guild.RankOfficer {
		return "", refusalFor(guild.InsufficientRank, "officers and the leader review applications")
	}
	for _, application := range mine.Applications {
		if application.PlayerID == applicantID {
			return fmt.Sprintf("Player Name: %s, Id: %s, Application Text: %s", application.Name, application.PlayerID,
				application.Text), nil
		}
	}
	return "", refusalFor(guild.PlayerNotFound, fmt.Sprintf("no application from %q", applicantID))
}

func (s *Server) reportAbuse(w http.ResponseWriter, r *http.Request, player string) {
	var request reportBody
	if !readGuildBody(w, r, &request) {
		return
	}
	if !request.AbuseType.Valid() {
		s.writeGuildFailure(w, player, refusalFor(guild.InvalidArgument, "pick what kind of abuse this is"))
		return
	}
	var description string
	err := refusalFor(guild.InvalidArgument, "kind must be guild or application")
	s.guilds.View(func(state *guild.State) {
		switch request.Kind {
		case ReportGuild:
			description, err = describeGuild(state, request.GuildID)
		case ReportApplication:
			description, err = describeApplication(state, player, request.ApplicantID)
		}
	})
	if err != nil {
		s.writeGuildFailure(w, player, err)
		return
	}
	entry := report.Entry{
		Time: s.now().Unix(), Reporter: player, IssueType: request.AbuseType, Description: description,
	}
	if err := s.reports.Append(entry); err != nil {
		s.log.Error("store abuse report", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not store the report")
		return
	}
	s.log.Info("abuse report", "reporter", player, "issue_type", entry.IssueType, "description", description)
	writeJSON(w, http.StatusOK, reportReply{Status: "received"})
}
