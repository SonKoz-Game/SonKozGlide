package router

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var sharedInfraRoots = []string{
	"adyen.com",
	"akamai.net",
	"akamaihd.net",
	"amazonaws.com",
	"arkoselabs.com",
	"auth0.com",
	"braintreegateway.com",
	"cloudfront.net",
	"cloudflare.com",
	"google.com",
	"googleapis.com",
	"googleusercontent.com",
	"gstatic.com",
	"gvt1.com",
	"gvt2.com",
	"hcaptcha.com",
	"paypal.com",
	"stripe.com",
	"stripe.network",
	"xsolla.com",
}

var legacyCoverage = []string{
	"discord.com",
	"api.discord.com",
	"canary.discord.com",
	"ptb.discord.com",
	"discordapp.com",
	"discordapp.net",
	"discord.gg",
	"gateway.discord.gg",
	"gateway-us-east1-b.discord.gg",
	"discordstatus.com",
	"discord.media",
	"latency.discord.media",
	"discordcdn.com",
	"cdn.discordapp.com",
	"media.discordapp.net",
	"images-ext-1.discordapp.net",
	"images-ext-4.discordapp.net",
	"rtc.discord.com",
	"media.tenor.com",
	"tenor.googleapis.com",
	"roblox.com",
	"apis.roblox.com",
	"gamejoin.roblox.com",
	"assetdelivery.roblox.com",
	"thumbnails.roblox.com",
	"rbxcdn.com",
	"setup.rbxcdn.com",
	"rbxtok.com",
	"rbximg.com",
	"rbxasm.com",
	"rbxmgr.com",
	"rbxapidev.com",
}

func projectShippedRuleset(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "rules", "rules.yaml"))
	if err != nil {
		t.Fatalf("shipped ruleset unreadable: %v", err)
	}

	rs, err := Compile(data)
	if err != nil {
		t.Fatalf("shipped ruleset does not compile: %v", err)
	}
	return rs.HostlistProjection()
}

func TestShippedHostlistCoversEveryLegacyHost(t *testing.T) {
	hosts := projectShippedRuleset(t)

	for _, want := range legacyCoverage {
		if !coveredByParent(want, hosts) {
			t.Errorf("%q is no longer covered by the generated hostlist", want)
		}
	}
}

func TestShippedHostlistExcludesSharedInfrastructure(t *testing.T) {
	hosts := projectShippedRuleset(t)

	listed := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		listed[host] = struct{}{}
	}

	for _, root := range sharedInfraRoots {
		if _, ok := listed[root]; ok {
			t.Errorf("%q is a shared-infrastructure root and must not be in the hostlist", root)
		}
	}
}

func TestShippedHostlistHasNoRedundantSubdomains(t *testing.T) {
	hosts := projectShippedRuleset(t)

	for i, host := range hosts {
		others := append(append([]string{}, hosts[:i]...), hosts[i+1:]...)
		if coveredByParent(host, others) {
			t.Errorf("%q is redundant; a listed parent already covers it", host)
		}
		if strings.TrimSpace(host) == "" {
			t.Error("the hostlist must not contain blank entries")
		}
	}
}
