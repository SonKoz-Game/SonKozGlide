package updater

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

const (
	fallbackVersion  = "v0.0.0-dev"
	RepoSlug         = "SonKoz-Game/SonKozGlide"
	ChecksumsAsset   = "checksums.txt"
	applyTimeout     = 10 * time.Minute
	progressInterval = 100 * time.Millisecond
)

const (
	StageDownload = "download"
	StageInstall  = "install"
)

var Version = fallbackVersion

type Progress struct {
	Stage      string `json:"stage"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
}

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

func newUpdater(report func(Progress)) (*selfupdate.Updater, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return nil, err
	}
	return selfupdate.NewUpdater(selfupdate.Config{
		Source:    progressSource{Source: source, report: report},
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: ChecksumsAsset},
	})
}

func Check() (*selfupdate.Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	up, err := newUpdater(nil)
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

func Apply(latest *selfupdate.Release, report func(Progress)) error {
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return err
	}
	return applyTo(latest, exe, report)
}

func applyTo(latest *selfupdate.Release, exe string, report func(Progress)) error {
	ctx, cancel := context.WithTimeout(context.Background(), applyTimeout)
	defer cancel()

	up, err := newUpdater(report)
	if err != nil {
		return err
	}
	return up.UpdateTo(ctx, latest, exe)
}

func RemoveLeftovers() {
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return
	}
	dir, name := filepath.Split(exe)
	old := filepath.Join(dir, "."+name+".old")
	for attempt := 0; attempt < 10; attempt++ {
		if err := os.Remove(old); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

type progressSource struct {
	selfupdate.Source
	report func(Progress)
}

func (s progressSource) DownloadReleaseAsset(ctx context.Context, rel *selfupdate.Release, assetID int64) (io.ReadCloser, error) {
	body, err := s.Source.DownloadReleaseAsset(ctx, rel, assetID)
	if err != nil || s.report == nil || assetID != rel.AssetID {
		return body, err
	}
	s.report(Progress{Stage: StageDownload, Total: int64(rel.AssetByteSize)})
	return &progressReader{ReadCloser: body, total: int64(rel.AssetByteSize), report: s.report}, nil
}

type progressReader struct {
	io.ReadCloser
	total    int64
	read     int64
	reported time.Time
	done     bool
	report   func(Progress)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.read += int64(n)
	if err == io.EOF && !r.done {
		r.done = true
		r.report(Progress{Stage: StageDownload, Downloaded: r.read, Total: r.total})
		r.report(Progress{Stage: StageInstall, Downloaded: r.read, Total: r.total})
	} else if err == nil && time.Since(r.reported) >= progressInterval {
		r.reported = time.Now()
		r.report(Progress{Stage: StageDownload, Downloaded: r.read, Total: r.total})
	}
	return n, err
}
