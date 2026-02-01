package characterdb

import (
	"fmt"
	"strings"
	"sync"

	"github.com/xackery/talkeq/tlog"
)

var (
	characters  = make(map[string]*Character)
	mu          sync.RWMutex
	onlineCount int
)

// Character represents a character inside EverQuest
type Character struct {
	IsOnline bool
	Identity string
	State    string
	Level    int
	Class    string
	Name     string
	Race     string
	Zone     string
	AcctID   int
	AcctName string
	LSID     int
	Status   int
}

// Characters is an list of character
type Characters []*Character

// CharactersOnline returns a string of online characters
func CharactersOnline(filter string) string {
	mu.RLock()
	defer mu.RUnlock()
	content := ""

	tlog.Debugf("[characterdb] iterating players (%d total) with filter '%s'", len(characters), filter)
	totalCount := 0
	hiddenCount := 0
	isTruncated := false
	for _, user := range characters {
		if totalCount >= 20 {
			isTruncated = true
		}
		if strings.Contains(user.State, "ANON") {
			hiddenCount++
			continue
		}
		if strings.Contains(user.State, "RolePlay") {
			hiddenCount++
			continue
		}
		/*if user.Status > 0 {
			hiddenCount++
			continue
		}*/

		if filter == "" {
			content += fmt.Sprintf("%s\n", user.Name)
			totalCount++
			continue
		}

		if !strings.Contains(user.Name, filter) &&
			!strings.Contains(user.Zone, filter) {
			continue
		}

		content += fmt.Sprintf("%s\n", user.Name)
		totalCount++
	}

	hiddenText := ""
	if hiddenCount > 0 {
		hiddenText = "(%d hidden) "
	}

	truncatedText := ""
	if isTruncated {
		truncatedText = "(truncated) "
	}

	if totalCount == 0 {
		content = fmt.Sprintf("There are 0 players %sonline.", hiddenText)
		return content
	}
	if filter == "" {
		content = fmt.Sprintf("There are %d players %sonline%s:\n%s", totalCount, hiddenText, truncatedText, content)
		return content
	}

	content = fmt.Sprintf("There are %d players %s%swho match '%s':\n%s", totalCount, hiddenText, truncatedText, filter, content)
	return content
}

// PlayerChange represents a player coming online or going offline
type PlayerChange struct {
	Name   string
	Class  string
	Level  int
	Zone   string
	Online bool // true = logged in, false = logged off
}

// SetCharacters sets the character db to provided argument and returns changes
func SetCharacters(req map[string]*Character) ([]PlayerChange, error) {
	mu.Lock()
	defer mu.Unlock()

	var changes []PlayerChange

	// Detect new players (online now but not before)
	for name, char := range req {
		if _, existed := characters[name]; !existed {
			changes = append(changes, PlayerChange{
				Name:   char.Name,
				Class:  char.Class,
				Level:  char.Level,
				Zone:   char.Zone,
				Online: true,
			})
		}
	}

	// Detect departed players (were online but not anymore)
	for name, char := range characters {
		if _, exists := req[name]; !exists {
			changes = append(changes, PlayerChange{
				Name:   char.Name,
				Class:  char.Class,
				Level:  char.Level,
				Zone:   char.Zone,
				Online: false,
			})
		}
	}

	characters = req
	onlineCount = len(characters)
	tlog.Debugf("[characterdb] onlineCount is %d", onlineCount)
	return changes, nil
}

// CharactersOnlineCount returns how many characters are reported online
func CharactersOnlineCount() int {
	mu.RLock()
	defer mu.RUnlock()
	return onlineCount
}

// SetCharactersOnlineCount sets how many characters are online
func SetCharactersOnlineCount(value int) {
	mu.Lock()
	defer mu.Unlock()
	onlineCount = value
}
