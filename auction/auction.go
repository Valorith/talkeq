package auction

import (
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	// ColorWTS is green for selling
	ColorWTS = 0x2ECC71
	// ColorWTB is blue for buying
	ColorWTB = 0x3498DB
	// ColorMixed is orange for mixed WTS/WTB
	ColorMixed = 0xE67E22
)

// ListingType represents the type of auction listing
type ListingType int

const (
	// ListingWTS is a Want To Sell listing
	ListingWTS ListingType = iota
	// ListingWTB is a Want To Buy listing
	ListingWTB
	// ListingMixed contains both WTS and WTB
	ListingMixed
)

// Item represents a parsed item from an auction message
type Item struct {
	Name  string
	Price string
}

// Listing represents a parsed auction listing
type Listing struct {
	Type    ListingType
	Seller  string
	Items   []Item
	RawText string
}

var (
	// pricePattern matches common price formats: 1000pp, 1.5k, 500p, 1kpp, etc.
	pricePattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:k)?\s*(?:pp|plat|p)\b`)
	// priceShortK matches "1k", "1.5k" without pp suffix
	priceShortK = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*k\b`)
)

// IsAuctionMessage returns true if the message contains WTS or WTB patterns
func IsAuctionMessage(message string) bool {
	upper := strings.ToUpper(message)
	return strings.Contains(upper, "WTS") || strings.Contains(upper, "WTB")
}

// Parse analyzes an auction message and extracts structured data
func Parse(seller string, message string) *Listing {
	upper := strings.ToUpper(message)
	hasWTS := strings.Contains(upper, "WTS")
	hasWTB := strings.Contains(upper, "WTB")

	listing := &Listing{
		Seller:  seller,
		RawText: message,
	}

	switch {
	case hasWTS && hasWTB:
		listing.Type = ListingMixed
	case hasWTB:
		listing.Type = ListingWTB
	default:
		listing.Type = ListingWTS
	}

	listing.Items = extractItems(message)
	return listing
}

// extractItems parses item names and prices from an auction message
func extractItems(message string) []Item {
	// Remove WTS/WTB prefixes for cleaner parsing
	cleaned := regexp.MustCompile(`(?i)\bWTS\b|\bWTB\b`).ReplaceAllString(message, "")
	cleaned = strings.TrimSpace(cleaned)

	if cleaned == "" {
		return nil
	}

	// Split by common delimiters used in EQ auction messages
	// Items are often separated by |, /, commas, or multiple spaces
	parts := regexp.MustCompile(`[|/]|,\s*|\s{2,}`).Split(cleaned, -1)

	var items []Item
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		item := Item{}
		// Try to extract price from this segment
		if match := pricePattern.FindStringSubmatchIndex(part); match != nil {
			item.Price = strings.TrimSpace(part[match[0]:match[1]])
			item.Name = strings.TrimSpace(part[:match[0]] + part[match[1]:])
		} else if match := priceShortK.FindStringSubmatchIndex(part); match != nil {
			item.Price = strings.TrimSpace(part[match[0]:match[1]])
			item.Name = strings.TrimSpace(part[:match[0]] + part[match[1]:])
		} else {
			item.Name = part
		}

		item.Name = strings.TrimSpace(strings.Trim(item.Name, "-–—"))
		if item.Name != "" {
			items = append(items, item)
		}
	}
	return items
}

// ToEmbed converts a Listing into a Discord embed
func (l *Listing) ToEmbed() *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Timestamp: time.Now().Format(time.RFC3339),
	}

	switch l.Type {
	case ListingWTS:
		embed.Color = ColorWTS
		embed.Author = &discordgo.MessageEmbedAuthor{
			Name: "📦 " + l.Seller + " — Want To Sell",
		}
	case ListingWTB:
		embed.Color = ColorWTB
		embed.Author = &discordgo.MessageEmbedAuthor{
			Name: "🛒 " + l.Seller + " — Want To Buy",
		}
	case ListingMixed:
		embed.Color = ColorMixed
		embed.Author = &discordgo.MessageEmbedAuthor{
			Name: "🔄 " + l.Seller + " — Buying & Selling",
		}
	}

	if len(l.Items) > 0 {
		var lines []string
		for _, item := range l.Items {
			if item.Price != "" {
				lines = append(lines, "• **"+item.Name+"** — "+item.Price)
			} else {
				lines = append(lines, "• **"+item.Name+"**")
			}
		}
		embed.Description = strings.Join(lines, "\n")
	} else {
		embed.Description = l.RawText
	}

	// Truncate if too long
	if len(embed.Description) > 2048 {
		embed.Description = embed.Description[:2045] + "..."
	}

	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: "EQ Auction",
	}

	return embed
}
