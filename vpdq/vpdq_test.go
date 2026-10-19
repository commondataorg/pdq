package vpdq

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// tools returns the ffmpeg and ffprobe to test with: VPDQ_FFMPEG and
// VPDQ_FFPROBE if set, else PATH. Without them the test is skipped, unless
// VPDQ_REQUIRE_FFMPEG is set, as CI sets it.
func tools(t *testing.T) Options {
	t.Helper()
	opts := Options{FFmpeg: os.Getenv("VPDQ_FFMPEG"), FFprobe: os.Getenv("VPDQ_FFPROBE")}
	if opts.FFmpeg == "" {
		opts.FFmpeg = "ffmpeg"
	}
	if opts.FFprobe == "" {
		opts.FFprobe = "ffprobe"
	}
	for _, bin := range []string{opts.FFmpeg, opts.FFprobe} {
		if _, err := exec.LookPath(bin); err != nil {
			if os.Getenv("VPDQ_REQUIRE_FFMPEG") != "" {
				t.Fatalf("%s not found: %v", bin, err)
			}
			t.Skipf("%s not found; set VPDQ_REQUIRE_FFMPEG to make this a failure", bin)
		}
	}
	return opts
}

// readReference parses the C++ vPDQ output in testdata:
// "frame,quality,hash,timestamp" per line, timestamp at 3 decimals.
func readReference(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func format(f Feature) string {
	return fmt.Sprintf("%d,%d,%s,%.3f", f.FrameNumber, f.Quality, f.Hash, f.Timestamp)
}

// TestHashFile_MatchesCPP checks every frame, and one hash per second,
// against the references in testdata (see testdata/README.md).
func TestHashFile_MatchesCPP(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("the references are x86-64 ffmpeg output; ffmpeg converts colour differently on other architectures")
	}
	base := tools(t)
	videos, err := filepath.Glob("testdata/*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) == 0 {
		t.Fatal("no fixture videos")
	}
	for _, video := range videos {
		name := strings.TrimSuffix(filepath.Base(video), ".mp4")
		for _, mode := range []struct {
			suffix string
			opts   Options
		}{
			{"every-frame", Options{EveryFrame: true}},
			{"1s", Options{SecondsPerHash: 1}},
		} {
			t.Run(name+"/"+mode.suffix, func(t *testing.T) {
				opts := mode.opts
				opts.FFmpeg, opts.FFprobe = base.FFmpeg, base.FFprobe
				got, err := HashFile(t.Context(), video, opts)
				if err != nil {
					t.Fatalf("HashFile: %v", err)
				}
				want := readReference(t, fmt.Sprintf("testdata/%s.%s.txt", name, mode.suffix))
				if len(got) != len(want) {
					t.Fatalf("%d features, C++ has %d", len(got), len(want))
				}
				for i := range want {
					if g := format(got[i]); g != want[i] {
						t.Errorf("feature %d: %s, C++ %s", i, g, want[i])
					}
				}
			})
		}
	}
}

func TestHashFile_WorkersKeepOrder(t *testing.T) {
	base := tools(t)
	run := func(workers int) []Feature {
		t.Helper()
		got, err := HashFile(t.Context(), "testdata/doorknob.mp4",
			Options{EveryFrame: true, Workers: workers, FFmpeg: base.FFmpeg, FFprobe: base.FFprobe})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if one, many := run(1), run(8); !reflect.DeepEqual(one, many) {
		t.Error("output depends on the worker count")
	}
}

// TestHashFile_StreamOrder checks that the video's position among the
// streams does not change the output.
func TestHashFile_StreamOrder(t *testing.T) {
	base := tools(t)
	audioFirst := filepath.Join(t.TempDir(), "audio-first.mp4")
	remux := exec.CommandContext(t.Context(), base.FFmpeg, "-nostdin", "-v", "error",
		"-f", "lavfi", "-i", "sine=duration=30", "-i", "testdata/doorknob.mp4",
		"-map", "0:a", "-map", "1:v", "-c:v", "copy", "-c:a", "aac", "-shortest", audioFirst)
	if out, err := remux.CombinedOutput(); err != nil {
		t.Fatalf("remux: %v: %s", err, out)
	}
	opts := base
	opts.EveryFrame = true
	want, err := HashFile(t.Context(), "testdata/doorknob.mp4", opts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(t.Context(), audioFirst, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audio-first remux gave %d features, video-first %d, or they differ", len(got), len(want))
	}
}

func TestHashFile_AudioOnly(t *testing.T) {
	base := tools(t)
	for _, ext := range []string{"mp3", "wav", "flac", "m4a"} {
		t.Run(ext, func(t *testing.T) {
			audio := filepath.Join(t.TempDir(), "audio."+ext)
			enc := exec.CommandContext(t.Context(), base.FFmpeg, "-nostdin", "-v", "error",
				"-f", "lavfi", "-i", "sine=duration=1", audio)
			if out, err := enc.CombinedOutput(); err != nil {
				t.Fatalf("encode: %v: %s", err, out)
			}
			if _, err := HashFile(t.Context(), audio, base); !errors.Is(err, ErrNoVideo) {
				t.Errorf("HashFile = %v, want ErrNoVideo", err)
			}
		})
	}
}

// coverArt writes a 64x64 PNG to attach as cover art.
func coverArt(t *testing.T, ffmpeg string) string {
	t.Helper()
	cover := filepath.Join(t.TempDir(), "cover.png")
	enc := exec.CommandContext(t.Context(), ffmpeg, "-nostdin", "-v", "error",
		"-f", "lavfi", "-i", "color=c=red:s=64x64", "-frames:v", "1", cover)
	if out, err := enc.CombinedOutput(); err != nil {
		t.Fatalf("encode cover: %v: %s", err, out)
	}
	return cover
}

func TestHashFile_SkipAttachedPictures(t *testing.T) {
	base := tools(t)
	cover := coverArt(t, base.FFmpeg)

	// An audio file whose only video stream is its cover art.
	for _, ext := range []string{"m4a", "mp3", "flac"} {
		t.Run(ext, func(t *testing.T) {
			audio := filepath.Join(t.TempDir(), "audio."+ext)
			enc := exec.CommandContext(t.Context(), base.FFmpeg, "-nostdin", "-v", "error",
				"-f", "lavfi", "-i", "sine=duration=1", "-i", cover,
				"-map", "0:a", "-map", "1:v", "-c:v", "png", "-disposition:v:0", "attached_pic", audio)
			if out, err := enc.CombinedOutput(); err != nil {
				t.Fatalf("encode: %v: %s", err, out)
			}
			// Like the C++, the default hashes the cover as a one-frame video.
			got, err := HashFile(t.Context(), audio, base)
			if err != nil || len(got) != 1 {
				t.Fatalf("default HashFile = %d features, %v; want the cover's one", len(got), err)
			}
			opts := base
			opts.SkipAttachedPictures = true
			if _, err := HashFile(t.Context(), audio, opts); !errors.Is(err, ErrNoVideo) {
				t.Errorf("HashFile skipping attached pictures = %v, want ErrNoVideo", err)
			}
		})
	}

	// A video that also carries cover art hashes as the video alone. Demuxers
	// put cover art after the tracks, so this cannot tell v:0 from V:0;
	// TestDecodeArgs_StreamSpec checks that the decode follows the option.
	t.Run("video with cover", func(t *testing.T) {
		withCover := filepath.Join(t.TempDir(), "with-cover.mp4")
		remux := exec.CommandContext(t.Context(), base.FFmpeg, "-nostdin", "-v", "error",
			"-i", "testdata/doorknob.mp4", "-i", cover,
			"-map", "0:v", "-map", "1:v", "-c:v:0", "copy", "-c:v:1", "png",
			"-disposition:v:1", "attached_pic", withCover)
		if out, err := remux.CombinedOutput(); err != nil {
			t.Fatalf("remux: %v: %s", err, out)
		}
		opts := base
		opts.EveryFrame = true
		want, err := HashFile(t.Context(), "testdata/doorknob.mp4", opts)
		if err != nil {
			t.Fatal(err)
		}
		opts.SkipAttachedPictures = true
		got, err := HashFile(t.Context(), withCover, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("with cover art gave %d features, without %d, or they differ", len(got), len(want))
		}
	})
}

func TestDecodeArgs_StreamSpec(t *testing.T) {
	st := stream{width: 64, height: 64, fps: 30}
	for _, spec := range []string{"v:0", "V:0"} {
		args := decodeArgs("file:/in", spec, st, 1, 2)
		i := slices.Index(args, "-map")
		if i < 0 || i+1 >= len(args) || args[i+1] != "0:"+spec {
			t.Errorf("decodeArgs(%q) maps %v, want -map 0:%s", spec, args, spec)
		}
	}
}

// TestDecodeArgs_Threads checks the thread count is an input option, so it
// reaches the decoder, and also sizes the filter graph.
func TestDecodeArgs_Threads(t *testing.T) {
	args := decodeArgs("file:/in", "v:0", stream{width: 64, height: 64, fps: 30}, 1, 3)
	in := slices.Index(args, "-i")
	for _, opt := range []string{"-threads", "-filter_threads"} {
		i := slices.Index(args, opt)
		if i < 0 || i > in || args[i+1] != "3" {
			t.Errorf("decodeArgs gave %v, want %s 3 before -i", args, opt)
		}
	}
}

func TestHashFile_EveryFrameIgnoresInterval(t *testing.T) {
	opts := tools(t)
	opts.EveryFrame = true
	opts.SecondsPerHash = math.NaN()
	if _, err := HashFile(t.Context(), "testdata/doorknob.mp4", opts); err != nil {
		t.Errorf("HashFile: %v", err)
	}
}

func TestHashFile_OnlyFirstFrameForLongInterval(t *testing.T) {
	opts := tools(t)
	opts.SecondsPerHash = 1e300
	got, err := HashFile(t.Context(), "testdata/doorknob.mp4", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FrameNumber != 0 {
		t.Errorf("got %d features, want frame 0 only", len(got))
	}
}

func TestHashFile_Errors(t *testing.T) {
	base := tools(t)
	dir := t.TempDir()
	notVideo := filepath.Join(dir, "not-a-video.mp4")
	if err := os.WriteFile(notVideo, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Both would hash a sibling file if the demuxers were not restricted.
	video, err := os.ReadFile("testdata/doorknob.mp4")
	if err != nil {
		t.Fatal(err)
	}
	concat := filepath.Join(dir, "upload")
	playlist := filepath.Join(dir, "upload.m3u8")
	for name, data := range map[string]string{
		filepath.Join(dir, "sibling.mp4"): string(video),
		concat:                            "ffconcat version 1.0\nfile sibling.mp4\n",
		playlist:                          "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\nsibling.mp4\n#EXT-X-ENDLIST\n",
	} {
		if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	withInterval := func(sph float64) Options {
		o := base
		o.SecondsPerHash = sph
		return o
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	for name, tc := range map[string]struct {
		ctx  context.Context
		path string
		opts Options
	}{
		"missing file":      {t.Context(), "testdata/does-not-exist.mp4", base},
		"not a video":       {t.Context(), notVideo, base},
		"truncated":         {t.Context(), "testdata/invalid/truncated.mp4", base},
		"negative interval": {t.Context(), "testdata/doorknob.mp4", withInterval(-1)},
		"NaN interval":      {t.Context(), "testdata/doorknob.mp4", withInterval(math.NaN())},
		"infinite interval": {t.Context(), "testdata/doorknob.mp4", withInterval(math.Inf(1))},
		"concat script":     {t.Context(), concat, base},
		"HLS playlist":      {t.Context(), playlist, base},
		"canceled":          {canceled, "testdata/doorknob.mp4", base},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := HashFile(tc.ctx, tc.path, tc.opts); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// TestHashFile_FFmpegDiesMidFrame checks that the error carries ffmpeg's
// stderr, not only the short read it caused.
func TestHashFile_FFmpegDiesMidFrame(t *testing.T) {
	opts := tools(t)
	opts.FFmpeg = filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nprintf 'partial'\necho 'decoder exploded' >&2\nexit 1\n"
	if err := os.WriteFile(opts.FFmpeg, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := HashFile(t.Context(), "testdata/doorknob.mp4", opts)
	if err == nil || !strings.Contains(err.Error(), "decoder exploded") {
		t.Errorf("HashFile error = %v, want ffmpeg's stderr in it", err)
	}
}

func TestHashFile_FrameTooLarge(t *testing.T) {
	base := tools(t)
	big := filepath.Join(t.TempDir(), "big.mkv")
	enc := exec.CommandContext(t.Context(), base.FFmpeg, "-nostdin", "-v", "error",
		"-f", "lavfi", "-i", "color=size=8200x8200:duration=0.04:rate=25", "-c:v", "ffv1", big)
	if out, err := enc.CombinedOutput(); err != nil {
		t.Fatalf("encode: %v: %s", err, out)
	}
	if _, err := HashFile(t.Context(), big, base); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("HashFile = %v, want ErrFrameTooLarge", err)
	}
}

func TestDefaultWorkers(t *testing.T) {
	for _, tc := range []struct {
		workers, w, h, procs, want int
	}{
		{0, 1280, 720, 16, 16},
		{0, 3840, 2160, 16, 11},
		{0, 8192, 8192, 16, 1},
		{0, 1280, 720, 0, 1},
		{4, 8192, 8192, 16, 4},
	} {
		got := defaultWorkers(tc.workers, stream{width: tc.w, height: tc.h}, tc.procs)
		if got != tc.want {
			t.Errorf("defaultWorkers(%d, %dx%d, %d) = %d, want %d", tc.workers, tc.w, tc.h, tc.procs, got, tc.want)
		}
	}
}

func TestCapped(t *testing.T) {
	c := &capped{max: 8}
	for _, s := range []string{"abc", "defgh", "ijk"} {
		if n, err := c.Write([]byte(s)); n != len(s) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", s, n, err)
		}
	}
	if got, want := c.String(), "abcdefgh [3 more bytes]"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestFrameRate(t *testing.T) {
	for _, tc := range []struct {
		avg, r string
		want   float64
		err    bool
	}{
		{"30000/1001", "30/1", 30000.0 / 1001.0, false},
		{"0/0", "25/1", 25, false},
		{"24/0", "24000/1001", 24000.0 / 1001.0, false},
		{"0/1", "0/0", 0, true},
		{"30", "30/1", 0, true},
		{"x/1", "30/1", 0, true},
	} {
		got, err := frameRate(tc.avg, tc.r)
		if (err != nil) != tc.err {
			t.Errorf("frameRate(%q, %q) error = %v, want error %v", tc.avg, tc.r, err, tc.err)
			continue
		}
		if !tc.err && got != tc.want {
			t.Errorf("frameRate(%q, %q) = %v, want %v", tc.avg, tc.r, got, tc.want)
		}
	}
}
