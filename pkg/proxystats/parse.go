package proxystats

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// StatsFilter is the one regex of the §8.8 table: the request sends it as
// /stats?filter=, and ParseEnvoy keeps only names it matches in full.
const StatsFilter = `^(control_plane\.connected_state|cluster_manager\.warming_clusters|listener_manager\.total_listeners_warming|` +
	`listener_manager\.lds\.update_(rejected|success)|cluster_manager\.cds\.update_(rejected|success)|` +
	`http\..*\.rds\..*\.update_rejected|cluster\..*\.update_rejected|server\.state|server\.uptime)$`

var statsRe = regexp.MustCompile(StatsFilter)

// Bounds on the Envoy names per proxy: maxNames are kept, maxParsedNames
// read, so the collector can keep grown ones first (design §8.8).
const (
	maxNames       = 64
	maxParsedNames = 4096
	maxNameLen     = 200
)

// serverStates are Envoy's server.state values.
var serverStates = []string{"live", "draining", "pre-initializing", "initializing"}

// ParseEnvoy reads Envoy's /stats text format ("name: value" lines). Names
// outside StatsFilter and values that are not integers are ignored.
func ParseEnvoy(body []byte) (PodStats, error) {
	var p PodStats
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 4096), MaxBody+1)
	for sc.Scan() {
		name, val, ok := strings.Cut(sc.Text(), ": ")
		if !ok || !statsRe.MatchString(name) {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			continue
		}
		switch name {
		case "control_plane.connected_state":
			c := v != 0
			p.Connected = &c
		case "cluster_manager.warming_clusters":
			p.WarmingClusters = &v
		case "listener_manager.total_listeners_warming":
			p.WarmingListeners = &v
		case "listener_manager.lds.update_rejected":
			p.LDS.Rejected = v
		case "listener_manager.lds.update_success":
			p.LDS.Success = v
		case "cluster_manager.cds.update_rejected":
			p.CDS.Rejected = v
		case "cluster_manager.cds.update_success":
			p.CDS.Success = v
		case "server.state":
			if v >= 0 && v < int64(len(serverStates)) {
				p.ServerState = serverStates[v]
			}
		case "server.uptime":
			p.Uptime = v
		default:
			// http.<prefix>.rds.<route config>.update_rejected or cluster.<name>.update_rejected.
			rest := strings.TrimSuffix(name, ".update_rejected")
			if r, ok := strings.CutPrefix(rest, "http."); ok {
				if _, rc, ok := strings.Cut(r, ".rds."); ok {
					p.RDS, p.RDSTruncated = addName(p.RDS, rc, v, p.RDSTruncated)
				}
			} else if c, ok := strings.CutPrefix(rest, "cluster."); ok {
				p.Clusters, p.ClustersTruncated = addName(p.Clusters, c, v, p.ClustersTruncated)
			}
		}
	}
	return p, sc.Err()
}

// addName records name's counter in m when the name is a plausible Envoy name
// and m is under maxParsedNames; a name over the cap sets cut.
func addName(m map[string]int64, name string, v int64, cut bool) (map[string]int64, bool) {
	if !validName(name) {
		return m, cut
	}
	if len(m) >= maxParsedNames {
		return m, true
	}
	if m == nil {
		m = map[string]int64{}
	}
	m[name] = v
	return m, cut
}

// validName accepts 1 to maxNameLen printable ASCII characters without spaces.
func validName(s string) bool {
	if s == "" || len(s) > maxNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}
