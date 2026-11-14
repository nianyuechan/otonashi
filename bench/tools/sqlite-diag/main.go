// Diagnostic program: sequential single-connection measurement of SQLite per-commit service time (no queuing interference),
// comparing 4 durability combinations on G:(HDD) and C:(NVMe).
package main

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

func bench(dir, name string, pragmas []string, n int) {
	path := dir + "/diag-" + name + ".db"
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
	u := url.URL{Scheme: "file", Path: "/" + path}
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		fmt.Println(name, "open error:", err)
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS t (k TEXT PRIMARY KEY, v TEXT) WITHOUT ROWID`); err != nil {
		fmt.Println(name, "create error:", err)
		return
	}
	durations := make([]time.Duration, 0, n)
	value := []byte("0123456789abcdef0123456789abcdef")
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("k:%d", i%20000)
		start := time.Now()
		if _, err := db.Exec(`INSERT INTO t(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, key, value); err != nil {
			fmt.Println(name, "write error:", err)
			return
		}
		durations = append(durations, time.Since(start))
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	total := time.Duration(0)
	for _, d := range durations {
		total += d
	}
	pct := func(p float64) float64 {
		return float64(sorted[int(float64(len(sorted)-1)*p)]) / 1e6
	}
	fmt.Printf("%-12s %s: n=%d avg=%.2fms p50=%.2fms p95=%.2fms max=%.2fms => %.1f commits/s\n",
		name, dir[:2], n, float64(total)/float64(n)/1e6, pct(0.5), pct(0.95), float64(sorted[len(sorted)-1])/1e6, float64(n)/total.Seconds())
}

func main() {
	common := []string{"busy_timeout(2500)"}
	modes := []struct {
		name    string
		pragmas []string
	}{
		{"full-del", append(append([]string{}, common...), "synchronous(FULL)")},
		{"normal-del", append(append([]string{}, common...), "synchronous(NORMAL)")},
		{"wal-normal", append(append([]string{}, common...), "synchronous(NORMAL)", "journal_mode(WAL)")},
		{"wal-full", append(append([]string{}, common...), "synchronous(FULL)", "journal_mode(WAL)")},
	}
	dirs := []string{"G:/ALL WORK/", os.TempDir() + "/"}
	const n = 100
	for round := 1; round <= 2; round++ {
		fmt.Printf("--- round %d ---\n", round)
		for _, dir := range dirs {
			for _, m := range modes {
				bench(dir, m.name, m.pragmas, n)
			}
		}
	}
}
