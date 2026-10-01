package updater

import (
	"context"
	"strings"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

const (
	fallbackVersion = "v0.0.0-dev"
	RepoSlug        = "SonKoz-Game/SonKozGlide"
	ChecksumsAsset  = "checksums.txt"
)

var Version = fallbackVersion

func GetVersion() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		version = fallbackVersion
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version
}

func newUpdater() (*selfupdate.Updater, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, err
	}
	return selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: ChecksumsAsset},
	})
}

func Check() (*selfupdate.Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	up, err := newUpdater()
	if err != nil {
		return nil, err
	}

	latest, found, err := up.DetectLatest(ctx, selfupdate.ParseSlug(RepoSlug))
	if err != nil {
		return nil, err
	}
	if !found || latest == nil {
		return nil, nil
	}
	if !latest.GreaterThan(GetVersion()) {
		return nil, nil
	}
	return latest, nil
}

func Apply(latest *selfupdate.Release) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	up, err := newUpdater()
	if err != nil {
		return err
	}
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return err
	}
	return up.UpdateTo(ctx, latest, exe)
}
