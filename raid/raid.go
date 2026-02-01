package raid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xackery/talkeq/config"
	"github.com/xackery/talkeq/request"
	"github.com/xackery/talkeq/tlog"
)

// RaidMember represents a parsed raid member from a dump
type RaidMember struct {
	Name        string `json:"characterName"`
	Level       int    `json:"level,omitempty"`
	Class       string `json:"class,omitempty"`
	GroupNumber int    `json:"groupNumber,omitempty"`
}

// AttendanceRecord is the payload sent to CW Raid Manager
type AttendanceRecord struct {
	CharacterName string `json:"characterName"`
	Level         *int   `json:"level,omitempty"`
	Class         string `json:"class,omitempty"`
	GroupNumber   *int   `json:"groupNumber,omitempty"`
	Status        string `json:"status"`
}

// AttendancePayload is the POST body for CW Raid Manager attendance API
type AttendancePayload struct {
	Note      string             `json:"note,omitempty"`
	EventType string             `json:"eventType,omitempty"`
	Records   []AttendanceRecord `json:"records"`
}

// Raid handles raid attendance integration
type Raid struct {
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
	config      config.Raid
	subscribers []func(interface{}) error
	collecting  bool
	dumpLines   []string
	dumpTimer   *time.Timer
}

// raidDumpStartPattern detects the beginning of a raid dump
// EQEmu #raidlist output typically starts with header lines like:
// "Raid Members:" or "Players in raid:"
var raidDumpStartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^Raid Members:`),
	regexp.MustCompile(`(?i)^Players on raid:`),
	regexp.MustCompile(`(?i)^#raidlist`),
	regexp.MustCompile(`(?i)^Raid roster`),
}

// raidMemberPattern parses individual raid member lines
// Typical format: "GroupNum | Name | Level | Class"
// or: "Name (Level Class) [Group X]"
// or simply: "Name Level Class"
var raidMemberPatterns = []*regexp.Regexp{
	// Format: "1 | Playername | 60 | Warrior"
	regexp.MustCompile(`^\s*(\d+)\s*\|\s*(\w+)\s*\|\s*(\d+)\s*\|\s*(\w[\w ]*\w)\s*$`),
	// Format: "Playername  60  Warrior  Group 1"
	regexp.MustCompile(`^\s*(\w+)\s+(\d+)\s+([\w ]+?)\s+(?:Group\s+)?(\d+)\s*$`),
	// Format: "Playername (60 Warrior)"
	regexp.MustCompile(`^\s*(\w+)\s+\((\d+)\s+([\w ]+?)\)\s*$`),
	// Format: "[Group X] Playername Level Class"
	regexp.MustCompile(`^\s*\[(?:Group\s+)?(\d+)\]\s*(\w+)\s+(\d+)\s+([\w ]+?)\s*$`),
	// Simple: "Playername" (name only, no level/class)
	regexp.MustCompile(`^\s*(\w{2,})\s*$`),
}

// raidDumpEndPattern detects end of raid dump
var raidDumpEndPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^\d+ total raid members`),
	regexp.MustCompile(`(?i)^End of raid`),
	regexp.MustCompile(`(?i)^---`),
}

// New creates a new Raid handler
func New(ctx context.Context, cfg config.Raid) (*Raid, error) {
	ctx, cancel := context.WithCancel(ctx)
	r := &Raid{
		ctx:    ctx,
		cancel: cancel,
		config: cfg,
	}

	if !cfg.IsEnabled {
		return r, nil
	}

	tlog.Debugf("[raid] initialized with API URL: %s, raid event: %s", cfg.APIURL, cfg.RaidEventID)
	return r, nil
}

// Subscribe registers a callback for outgoing messages (e.g., Discord notifications)
func (r *Raid) Subscribe(ctx context.Context, onMessage func(interface{}) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subscribers = append(r.subscribers, onMessage)
	return nil
}

// ProcessTelnetLine handles a single line from telnet to detect raid dumps
func (r *Raid) ProcessTelnetLine(line string) {
	if !r.config.IsEnabled {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}

	// Check if this line starts a raid dump
	if !r.collecting {
		for _, pattern := range raidDumpStartPatterns {
			if pattern.MatchString(trimmed) {
				tlog.Infof("[raid] detected raid dump start: %s", trimmed)
				r.collecting = true
				r.dumpLines = []string{}
				// Set a timeout to flush the dump if no end marker is found
				if r.dumpTimer != nil {
					r.dumpTimer.Stop()
				}
				r.dumpTimer = time.AfterFunc(10*time.Second, func() {
					r.mu.Lock()
					defer r.mu.Unlock()
					if r.collecting {
						tlog.Infof("[raid] dump collection timed out, processing %d lines", len(r.dumpLines))
						r.finishDump()
					}
				})
				return
			}
		}
		return
	}

	// Check if this line ends a raid dump
	for _, pattern := range raidDumpEndPatterns {
		if pattern.MatchString(trimmed) {
			tlog.Infof("[raid] detected raid dump end: %s", trimmed)
			if r.dumpTimer != nil {
				r.dumpTimer.Stop()
			}
			r.finishDump()
			return
		}
	}

	// Accumulate dump lines
	r.dumpLines = append(r.dumpLines, trimmed)
}

// finishDump processes accumulated dump lines (must be called with lock held)
func (r *Raid) finishDump() {
	lines := r.dumpLines
	r.collecting = false
	r.dumpLines = nil

	if len(lines) == 0 {
		tlog.Warnf("[raid] empty raid dump, ignoring")
		return
	}

	members := ParseRaidDump(lines)
	if len(members) == 0 {
		tlog.Warnf("[raid] no members parsed from raid dump (%d lines)", len(lines))
		return
	}

	tlog.Infof("[raid] parsed %d raid members from dump", len(members))

	if r.config.AutoPost {
		go r.postAttendance(members)
	}
}

// ParseRaidDump parses raid member entries from dump lines
func ParseRaidDump(lines []string) []RaidMember {
	var members []RaidMember
	seen := make(map[string]bool)

	for _, line := range lines {
		member, ok := parseMemberLine(line)
		if !ok {
			continue
		}
		// Deduplicate
		if seen[strings.ToLower(member.Name)] {
			continue
		}
		seen[strings.ToLower(member.Name)] = true
		members = append(members, member)
	}
	return members
}

func parseMemberLine(line string) (RaidMember, bool) {
	// Pattern 1: "GroupNum | Name | Level | Class"
	if matches := raidMemberPatterns[0].FindStringSubmatch(line); matches != nil {
		group, _ := strconv.Atoi(matches[1])
		level, _ := strconv.Atoi(matches[3])
		return RaidMember{
			Name:        matches[2],
			Level:       level,
			Class:       normalizeClass(matches[4]),
			GroupNumber: group,
		}, true
	}

	// Pattern 2: "Name Level Class Group"
	if matches := raidMemberPatterns[1].FindStringSubmatch(line); matches != nil {
		level, _ := strconv.Atoi(matches[2])
		group, _ := strconv.Atoi(matches[4])
		return RaidMember{
			Name:        matches[1],
			Level:       level,
			Class:       normalizeClass(matches[3]),
			GroupNumber: group,
		}, true
	}

	// Pattern 3: "Name (Level Class)"
	if matches := raidMemberPatterns[2].FindStringSubmatch(line); matches != nil {
		level, _ := strconv.Atoi(matches[2])
		return RaidMember{
			Name:  matches[1],
			Level: level,
			Class: normalizeClass(matches[3]),
		}, true
	}

	// Pattern 4: "[Group X] Name Level Class"
	if matches := raidMemberPatterns[3].FindStringSubmatch(line); matches != nil {
		group, _ := strconv.Atoi(matches[1])
		level, _ := strconv.Atoi(matches[3])
		return RaidMember{
			Name:        matches[2],
			Level:       level,
			Class:       normalizeClass(matches[4]),
			GroupNumber: group,
		}, true
	}

	// Pattern 5: "Name" (simple name-only)
	if matches := raidMemberPatterns[4].FindStringSubmatch(line); matches != nil {
		name := matches[1]
		// Skip common header/footer words
		lower := strings.ToLower(name)
		if lower == "name" || lower == "player" || lower == "class" || lower == "level" ||
			lower == "group" || lower == "raid" || lower == "members" || lower == "total" {
			return RaidMember{}, false
		}
		return RaidMember{
			Name: name,
		}, true
	}

	return RaidMember{}, false
}

// postAttendance sends parsed raid members to CW Raid Manager API
func (r *Raid) postAttendance(members []RaidMember) {
	records := make([]AttendanceRecord, 0, len(members))
	for _, m := range members {
		rec := AttendanceRecord{
			CharacterName: m.Name,
			Class:         m.Class,
			Status:        "PRESENT",
		}
		if m.Level > 0 {
			level := m.Level
			rec.Level = &level
		}
		if m.GroupNumber > 0 {
			group := m.GroupNumber
			rec.GroupNumber = &group
		}
		records = append(records, rec)
	}

	payload := AttendancePayload{
		Note:      fmt.Sprintf("Auto-synced from TalkEQ at %s", time.Now().UTC().Format(time.RFC3339)),
		EventType: "LOG",
		Records:   records,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		tlog.Errorf("[raid] failed to marshal attendance payload: %s", err)
		return
	}

	url := fmt.Sprintf("%s/api/attendance/raid/%s", strings.TrimRight(r.config.APIURL, "/"), r.config.RaidEventID)
	tlog.Infof("[raid] posting attendance for %d members to %s", len(members), url)

	req, err := http.NewRequestWithContext(r.ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		tlog.Errorf("[raid] failed to create HTTP request: %s", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf("cwraid_token=%s", r.config.APIToken))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		tlog.Errorf("[raid] failed to POST attendance: %s", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		tlog.Infof("[raid] attendance posted successfully (status %d)", resp.StatusCode)
		if r.config.NotifyDiscord && r.config.DiscordChannelID != "" {
			r.sendDiscordNotification(members)
		}
	} else {
		tlog.Errorf("[raid] attendance POST failed (status %d): %s", resp.StatusCode, string(respBody))
	}
}

// sendDiscordNotification sends a Discord embed about synced attendance
func (r *Raid) sendDiscordNotification(members []RaidMember) {
	r.mu.RLock()
	subs := r.subscribers
	r.mu.RUnlock()

	if len(subs) == 0 {
		return
	}

	// Build class summary
	classCounts := make(map[string]int)
	for _, m := range members {
		cls := m.Class
		if cls == "" {
			cls = "Unknown"
		}
		classCounts[cls]++
	}

	var classBreakdown strings.Builder
	for cls, count := range classCounts {
		if classBreakdown.Len() > 0 {
			classBreakdown.WriteString(", ")
		}
		classBreakdown.WriteString(fmt.Sprintf("%s: %d", cls, count))
	}

	// Build player list (first 20)
	var playerList strings.Builder
	for i, m := range members {
		if i >= 20 {
			playerList.WriteString(fmt.Sprintf("\n...and %d more", len(members)-20))
			break
		}
		if i > 0 {
			playerList.WriteString(", ")
		}
		playerList.WriteString(m.Name)
	}

	msg := fmt.Sprintf("📋 **Raid Attendance Synced**\n"+
		"**Players:** %d\n"+
		"**Classes:** %s\n"+
		"**Roster:** %s\n"+
		"*Synced at %s UTC*",
		len(members),
		classBreakdown.String(),
		playerList.String(),
		time.Now().UTC().Format("15:04:05"),
	)

	discordReq := request.DiscordSend{
		Ctx:       r.ctx,
		ChannelID: r.config.DiscordChannelID,
		Message:   msg,
	}

	for i, s := range subs {
		err := s(discordReq)
		if err != nil {
			tlog.Warnf("[raid->discord subscriber %d] failed: %s", i, err)
		}
	}
}

// normalizeClass maps EQ class names to CW Raid Manager CharacterClass enum values
func normalizeClass(input string) string {
	classMap := map[string]string{
		"bard":          "BARD",
		"beastlord":     "BEASTLORD",
		"berserker":     "BERSERKER",
		"cleric":        "CLERIC",
		"druid":         "DRUID",
		"enchanter":     "ENCHANTER",
		"magician":      "MAGICIAN",
		"monk":          "MONK",
		"necromancer":   "NECROMANCER",
		"paladin":       "PALADIN",
		"ranger":        "RANGER",
		"rogue":         "ROGUE",
		"shadow knight": "SHADOWKNIGHT",
		"shadowknight":  "SHADOWKNIGHT",
		"shaman":        "SHAMAN",
		"warrior":       "WARRIOR",
		"wizard":        "WIZARD",
	}
	normalized := strings.TrimSpace(strings.ToLower(input))
	if val, ok := classMap[normalized]; ok {
		return val
	}
	return "UNKNOWN"
}
