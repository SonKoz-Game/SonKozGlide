package engine

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/SonKoz-Game/SonKozGlide/internal/router"
)

func ApplyRuleset(rulesYAML []byte) error {
	started := time.Now()
	rs, err := router.Compile(rulesYAML)
	if err != nil {
		appendEngineLog("routing: ruleset gecersiz: " + err.Error())
		return err
	}

	listPath := filepath.Join(getTargetDir(), "list.txt")
	if err := router.WriteHostlist(listPath, rs); err != nil {
		appendEngineLog("routing: hostlist yazilamadi: " + err.Error())
		return fmt.Errorf("hostlist yazilamadi: %w", err)
	}

	recordRulesetStats(rs.Version, rs.Counts.Domains, rs.PlatformNames, len(rs.HostlistProjection()), time.Since(started))
	appendEngineLog(fmt.Sprintf("routing: hostlist regenerated version=%s domains=%d keywords=%d cidr=%d",
		rs.Version, rs.Counts.Domains, rs.Counts.Keywords, rs.Counts.CIDRs))
	return nil
}
