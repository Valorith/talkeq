package auction

import (
	"testing"
)

func TestIsAuctionMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"WTS Sword of Fire 500pp", true},
		{"WTB Cloak of Flames", true},
		{"WTS/WTB Various items", true},
		{"Hello everyone", false},
		{"wts cheap gear", true},
	}
	for _, tt := range tests {
		if got := IsAuctionMessage(tt.msg); got != tt.want {
			t.Errorf("IsAuctionMessage(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		seller  string
		msg     string
		wantType ListingType
		wantItems int
	}{
		{
			name:      "simple WTS",
			seller:    "Playername",
			msg:       "WTS Sword of Fire 500pp",
			wantType:  ListingWTS,
			wantItems: 1,
		},
		{
			name:      "WTB message",
			seller:    "Buyer",
			msg:       "WTB Cloak of Flames",
			wantType:  ListingWTB,
			wantItems: 1,
		},
		{
			name:      "mixed message",
			seller:    "Trader",
			msg:       "WTS Sword 500pp / WTB Shield",
			wantType:  ListingMixed,
			wantItems: 2,
		},
		{
			name:      "multiple items with pipe delimiter",
			seller:    "Seller",
			msg:       "WTS Iron Sword 100pp | Steel Shield 200pp | Magic Ring 1kpp",
			wantType:  ListingWTS,
			wantItems: 3,
		},
		{
			name:      "items with comma delimiter",
			seller:    "Seller",
			msg:       "WTS Iron Sword 100pp, Steel Shield 200pp",
			wantType:  ListingWTS,
			wantItems: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listing := Parse(tt.seller, tt.msg)
			if listing.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", listing.Type, tt.wantType)
			}
			if len(listing.Items) != tt.wantItems {
				t.Errorf("Items count = %d, want %d. Items: %+v", len(listing.Items), tt.wantItems, listing.Items)
			}
		})
	}
}

func TestParseExtractsPrice(t *testing.T) {
	listing := Parse("Seller", "WTS Sword of Fire 500pp")
	if len(listing.Items) == 0 {
		t.Fatal("expected at least 1 item")
	}
	if listing.Items[0].Price == "" {
		t.Error("expected price to be extracted")
	}
	if listing.Items[0].Name == "" {
		t.Error("expected item name to be extracted")
	}
}

func TestToEmbed(t *testing.T) {
	listing := Parse("TestSeller", "WTS Sword of Fire 500pp | Magic Ring 1k")
	embed := listing.ToEmbed()
	if embed == nil {
		t.Fatal("expected embed")
	}
	if embed.Color != ColorWTS {
		t.Errorf("expected green color for WTS, got %d", embed.Color)
	}
	if embed.Author == nil || embed.Author.Name == "" {
		t.Error("expected author to be set")
	}
	if embed.Description == "" {
		t.Error("expected description")
	}
}
