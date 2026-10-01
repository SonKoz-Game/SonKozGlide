package updater

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

type chunkReader struct {
	data  []byte
	chunk int
	delay time.Duration
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(r.delay)
	n := copy(p[:min(len(p), r.chunk)], r.data)
	r.data = r.data[n:]
	return n, nil
}

type fakeSource struct {
	assets map[int64][]byte
}

func (f fakeSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return nil, nil
}

func (f fakeSource) DownloadReleaseAsset(_ context.Context, _ *selfupdate.Release, id int64) (io.ReadCloser, error) {
	return io.NopCloser(&chunkReader{data: f.assets[id], chunk: 1024, delay: 5 * time.Millisecond}), nil
}

func TestProgressSourceReportsTheBinaryDownload(t *testing.T) {
	binary := bytes.Repeat([]byte{1}, 64*1024)
	rel := &selfupdate.Release{AssetID: 7, AssetByteSize: len(binary), ValidationAssetID: 8}

	var events []Progress
	src := progressSource{
		Source: fakeSource{assets: map[int64][]byte{7: binary, 8: []byte("checksums")}},
		report: func(p Progress) { events = append(events, p) },
	}

	body, err := src.DownloadReleaseAsset(context.Background(), rel, 7)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil || !bytes.Equal(data, binary) {
		t.Fatalf("the wrapped body must pass the bytes through unchanged: %v", err)
	}

	if len(events) < 4 {
		t.Fatalf("expected start, intermediate, complete and install events, got %+v", events)
	}
	if len(events) > 20 {
		t.Fatalf("progress must be throttled, got %d events for 64 reads", len(events))
	}
	first, done, install := events[0], events[len(events)-2], events[len(events)-1]
	if first.Stage != StageDownload || first.Downloaded != 0 || first.Total != int64(len(binary)) {
		t.Fatalf("unexpected first event %+v", first)
	}
	if done.Stage != StageDownload || done.Downloaded != int64(len(binary)) {
		t.Fatalf("the download must finish at 100%%, got %+v", done)
	}
	if install.Stage != StageInstall {
		t.Fatalf("the last event must announce the install, got %+v", install)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Downloaded < events[i-1].Downloaded {
			t.Fatalf("progress went backwards: %+v", events)
		}
	}

	count := len(events)
	body, _ = src.DownloadReleaseAsset(context.Background(), rel, 8)
	_, _ = io.ReadAll(body)
	if len(events) != count {
		t.Fatal("the checksum file must not be reported as the update download")
	}
}

func TestProgressSourceWithoutReporterIsTransparent(t *testing.T) {
	src := progressSource{Source: fakeSource{assets: map[int64][]byte{1: []byte("x")}}}
	body, err := src.DownloadReleaseAsset(context.Background(), &selfupdate.Release{AssetID: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body.(*progressReader); ok {
		t.Fatal("update checks must not wrap the download")
	}
}
