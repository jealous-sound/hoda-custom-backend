package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mustDefault(t *testing.T) *Catalog {
	t.Helper()
	c, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEmbeddedCatalogLoads(t *testing.T) {
	c := mustDefault(t)
	for _, id := range []string{"recruit", "champion", "speed_kills", "peasant", "tutorial_consume"} {
		if _, ok := c.Pack(id); !ok {
			t.Errorf("pack %s missing", id)
		}
	}
	champion, _ := c.Pack("champion")
	if champion.Paid == nil || champion.Paid.Currency != CurrencyGems || champion.Paid.Price != 30 {
		t.Errorf("champion paid offer = %+v", champion.Paid)
	}
	if champion.Voucher < 0 || c.Vouchers[champion.Voucher] != "champion" {
		t.Errorf("champion voucher = %d", champion.Voucher)
	}
	if c.FlashCount == 0 {
		t.Error("no flash rotation packs")
	}
}

func TestDefaultPoolSkipsWispsAndBanners(t *testing.T) {
	c := mustDefault(t)
	for rarity := RarityCommon; rarity <= RarityLegendary; rarity++ {
		for _, index := range c.RarityPool(rarity) {
			hero := c.Heroes[index]
			if !hero.Obtainable || hero.Rarity != rarity || bytes.Contains([]byte(hero.ID), []byte("WISP")) ||
				bytes.Contains([]byte(hero.ID), []byte("BANNER")) {
				t.Fatalf("%s should not be in the %d pool", hero.ID, rarity)
			}
		}
	}
	if len(c.TaggedPool("WISP", RarityCommon))+len(c.TaggedPool("WISP", RarityUncommon)) == 0 {
		t.Error("no wisps for the wisp packs")
	}
}

func TestSchedulesUseUTCAndWrapTheYear(t *testing.T) {
	c := mustDefault(t)
	wintersend, _ := c.Pack("wintersend")
	for _, day := range []string{"2026-12-10", "2026-12-31", "2027-01-15"} {
		at, _ := time.Parse("2006-01-02", day)
		if !c.PackOnSale(wintersend, at) {
			t.Errorf("wintersend should be on sale on %s", day)
		}
	}
	off, _ := time.Parse("2006-01-02", "2027-01-16")
	if c.PackOnSale(wintersend, off) {
		t.Error("wintersend should be off sale on 2027-01-16")
	}
}

func TestFlashPacksRotateOnePerPeriod(t *testing.T) {
	c := mustDefault(t)
	at := time.Unix(1_800_000_000, 0)
	onSale := 0
	for i := range c.Packs {
		if c.Packs[i].FlashIndex >= 0 && c.PackOnSale(&c.Packs[i], at) {
			onSale++
		}
	}
	if onSale != 1 {
		t.Errorf("%d flash packs on sale, want 1", onSale)
	}
}

func TestEmbeddedDataMatchesClientCheckout(t *testing.T) {
	clientData := filepath.Join("..", "..", "..", "hoda-custom-client", "data")
	if dir := os.Getenv("HOD_CLIENT_DIR"); dir != "" {
		clientData = filepath.Join(dir, "data")
	}
	if _, err := os.Stat(clientData); err != nil {
		t.Skipf("no client checkout at %s", clientData)
	}
	entries, err := embedded.ReadDir("data")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		ours, _ := embedded.ReadFile("data/" + entry.Name())
		theirs, err := os.ReadFile(filepath.Join(clientData, entry.Name()))
		if err != nil {
			t.Errorf("%s: %v", entry.Name(), err)
			continue
		}
		if !bytes.Equal(ours, theirs) {
			t.Errorf("%s differs from the client; run scripts/sync-client-data.sh", entry.Name())
		}
	}
}
