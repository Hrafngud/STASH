package linuxproc

import (
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseMemInfoWhitespaceAndSemantics(t *testing.T) {
	t.Parallel()
	snapshot, err := ParseMemInfo(fixture(t, "meminfo_whitespace.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TotalBytes != 8192*1024 || snapshot.AvailableBytes != 2048*1024 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, input := range [][]byte{
		fixture(t, "meminfo_unavailable.txt"),
		[]byte("MemTotal: 1 MB\nMemAvailable: 1 kB\n"),
		[]byte("MemTotal: 1 kB\nMemTotal: 1 kB\nMemAvailable: 1 kB\n"),
		[]byte("MemTotal: 1 kB\nMemAvailable: 2 kB\n"),
	} {
		if _, err := ParseMemInfo(input); err == nil {
			t.Fatalf("ParseMemInfo(%q) succeeded", input)
		}
	}
}

func TestParseNetDevWhitespaceDiscoveryAndMalformedData(t *testing.T) {
	t.Parallel()
	snapshot, err := ParseNetDev(fixture(t, "netdev_base.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 2 || snapshot["enp4s0"] != (NetworkCounters{4000, 40, 8000, 80}) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, input := range [][]byte{
		fixture(t, "netdev_unavailable.txt"),
		[]byte("eth0: 1 2 3\n"),
		[]byte("eth0: 1 2 0 0 0 0 0 0 3 nope 0 0 0 0 0 0\n"),
		[]byte("eth0: 1 2 0 0 0 0 0 0 3 4 0 0 0 0 0 0\neth0: 1 2 0 0 0 0 0 0 3 4 0 0 0 0 0 0\n"),
	} {
		if _, err := ParseNetDev(input); err == nil {
			t.Fatalf("ParseNetDev(%q) succeeded", input)
		}
	}
}

func TestParseDiskStatsWhitespaceDiscoveryAndMalformedData(t *testing.T) {
	t.Parallel()
	snapshot, err := ParseDiskStats(fixture(t, "diskstats_base.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 2 || snapshot["nvme0n1"] != (DiskCounters{ReadsCompleted: 100, ReadSectors: 1000, WritesCompleted: 200, WrittenSectors: 3000}) {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, input := range [][]byte{
		fixture(t, "diskstats_unavailable.txt"),
		[]byte("259 0 nvme0n1 1 2\n"),
		[]byte("259 0 nvme0n1 nope 0 1 0 2 0 3 0 0 0 0\n"),
		[]byte(strings.Repeat("1 ", 2) + "nvme0n1 1 0 1 0 2 0 3 0 0 0 0\n" + strings.Repeat("1 ", 2) + "nvme0n1 1 0 1 0 2 0 3 0 0 0 0\n"),
	} {
		if _, err := ParseDiskStats(input); err == nil {
			t.Fatalf("ParseDiskStats(%q) succeeded", input)
		}
	}
}
