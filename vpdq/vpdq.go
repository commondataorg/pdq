// Package vpdq computes vPDQ video hashes without CGo, decoding through an
// ffmpeg subprocess. README.md describes the output it reproduces and why
// each ffmpeg setting is what it is.
package vpdq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/commondataorg/pdq"
)

// Feature is the PDQ hash of one sampled frame.
type Feature struct {
	// Hash is the frame's PDQ hash as 64 hex characters.
	Hash string `json:"hash"`
	// FrameNumber is the frame's index in decode order, from 0.
	FrameNumber int `json:"frame_number"`
	// Quality is the PDQ quality score, 0 to 100.
	Quality int `json:"quality"`
	// Timestamp is FrameNumber divided by the stream's frame rate, in
	// seconds, computed in float32.
	Timestamp float32 `json:"timestamp"`
}

// Options configures HashFile. The zero value hashes one frame per second
// using ffmpeg and ffprobe from PATH.
type Options struct {
	// SecondsPerHash is the sampling interval: every frame whose index is a
	// multiple of int(SecondsPerHash * fps) is hashed. 0 means 1; it must
	// be finite and not negative unless EveryFrame is set.
	SecondsPerHash float64
	// EveryFrame hashes every decoded frame, ignoring SecondsPerHash.
	EveryFrame bool
	// FFmpeg and FFprobe name the binaries; empty means "ffmpeg" and
	// "ffprobe" from PATH.
	FFmpeg, FFprobe string
	// Workers is how many frames are hashed at once. Each worker holds
	// about 11 bytes per pixel while it hashes a frame, so 0 means
	// GOMAXPROCS, reduced to keep that under 1 GiB in total.
	Workers int
	// SkipAttachedPictures hashes the first video stream that is not an
	// attached picture, such as cover art, so a file with only cover art
	// returns ErrNoVideo. The C++ hashes the first video stream of any kind,
	// so this changes the output for those files.
	SkipAttachedPictures bool
}

// ErrNoVideo is returned when a file has no video stream.
var ErrNoVideo = errors.New("vpdq: no video stream")

// ErrFrameTooLarge is returned when a video's frames are over maxFramePixels.
var ErrFrameTooLarge = errors.New("vpdq: video frame is over the size limit")

// maxFramePixels bounds the frame size HashFile accepts: 8192x8192 covers
// 8K UHD and HEVC's largest level.
const maxFramePixels = 8192 * 8192

// workerBudget bounds the memory the default worker count holds.
const workerBudget = 1 << 30

// bytesPerWorkerPixel is what each worker holds per pixel: the RGB24 frame
// and two float32 buffers.
const bytesPerWorkerPixel = 11

// maxStderr bounds how much of ffmpeg's or ffprobe's stderr an error keeps.
const maxStderr = 4096

// HashFile hashes the video at path. A decoding error anywhere in the stream
// fails the whole file, and so does a container format outside inputArgs'
// list. A file with no video stream returns ErrNoVideo.
func HashFile(ctx context.Context, path string, opts Options) ([]Feature, error) {
	sph := opts.SecondsPerHash
	if !opts.EveryFrame && (sph < 0 || math.IsNaN(sph) || math.IsInf(sph, 0)) {
		return nil, fmt.Errorf("vpdq: SecondsPerHash %v is not a finite, non-negative number", sph)
	}
	if sph == 0 {
		sph = 1
	}
	ffmpeg, ffprobe := opts.FFmpeg, opts.FFprobe
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("vpdq: %w", err)
	}
	// A file: URL keeps a name like "concat:a|b" from selecting a protocol.
	input := "file:" + abs

	// ffmpeg's V stream type is video other than attached pictures.
	spec := "v:0"
	if opts.SkipAttachedPictures {
		spec = "V:0"
	}
	st, err := probe(ctx, ffprobe, input, spec)
	if err != nil {
		return nil, err
	}
	step := 1
	if !opts.EveryFrame {
		step = max(int(min(sph*st.fps, math.MaxInt32)), 1)
	}
	procs := runtime.GOMAXPROCS(0)
	return decode(ctx, ffmpeg, input, spec, st, step, procs, defaultWorkers(opts.Workers, st, procs))
}

// defaultWorkers returns workers if it is set, else procs reduced so the
// workers' frames fit in workerBudget.
func defaultWorkers(workers int, st stream, procs int) int {
	if workers > 0 {
		return workers
	}
	perWorker := int64(st.width) * int64(st.height) * bytesPerWorkerPixel
	return int(max(1, min(int64(procs), workerBudget/perWorker)))
}

type stream struct {
	width, height int
	fps           float64
}

// inputArgs restrict both tools to local files and to demuxers that read
// only their input, so it cannot be a playlist or concat script naming other
// files. The audio demuxers let an audio-only file return ErrNoVideo.
var inputArgs = []string{
	"-protocol_whitelist", "file",
	"-format_whitelist", "mov,matroska,mpegts,avi,flv,mpeg,asf,ogg,gif," +
		"apng,dv,h264,hevc,ivf,m4v,mjpeg,mpegvideo,mxf,nut,obu,rm,yuv4mpegpipe," +
		"mp3,aac,wav,flac",
}

// probe reads the size and frame rate of the video stream spec selects.
func probe(ctx context.Context, ffprobe, input, spec string) (stream, error) {
	stderr := &capped{max: maxStderr}
	args := append([]string{"-v", "error"}, inputArgs...)
	args = append(args, "-select_streams", spec,
		"-show_entries", "stream=width,height,avg_frame_rate,r_frame_rate", "-of", "json", input)
	cmd := exec.CommandContext(ctx, ffprobe, args...)
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		return stream{}, fmt.Errorf("vpdq: ffprobe %s: %w: %s", input, err, stderr)
	}
	var p struct {
		Streams []struct {
			Width        int    `json:"width"`
			Height       int    `json:"height"`
			AvgFrameRate string `json:"avg_frame_rate"`
			RFrameRate   string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return stream{}, fmt.Errorf("vpdq: parse ffprobe output: %w", err)
	}
	if len(p.Streams) == 0 {
		return stream{}, ErrNoVideo
	}
	s := p.Streams[0]
	if s.Width <= 0 || s.Height <= 0 {
		return stream{}, fmt.Errorf("vpdq: video stream is %dx%d", s.Width, s.Height)
	}
	if int64(s.Width)*int64(s.Height) > maxFramePixels {
		return stream{}, fmt.Errorf("%w: %dx%d, limit %d pixels", ErrFrameTooLarge, s.Width, s.Height, maxFramePixels)
	}
	fps, err := frameRate(s.AvgFrameRate, s.RFrameRate)
	if err != nil {
		return stream{}, err
	}
	return stream{width: s.Width, height: s.Height, fps: fps}, nil
}

// frameRate takes avg_frame_rate, falling back to r_frame_rate when either
// part of it is zero.
func frameRate(avg, r string) (float64, error) {
	num, den, err := parseRational(avg)
	if err != nil {
		return 0, err
	}
	if num == 0 || den == 0 {
		if num, den, err = parseRational(r); err != nil {
			return 0, err
		}
	}
	if num == 0 || den == 0 {
		return 0, errors.New("vpdq: video frame rate is zero")
	}
	return float64(num) / float64(den), nil
}

func parseRational(s string) (num, den int64, err error) {
	n, d, ok := strings.Cut(s, "/")
	if !ok {
		return 0, 0, fmt.Errorf("vpdq: malformed frame rate %q", s)
	}
	if num, err = strconv.ParseInt(n, 10, 64); err != nil {
		return 0, 0, fmt.Errorf("vpdq: malformed frame rate %q: %w", s, err)
	}
	if den, err = strconv.ParseInt(d, 10, 64); err != nil {
		return 0, 0, fmt.Errorf("vpdq: malformed frame rate %q: %w", s, err)
	}
	return num, den, nil
}

// filter selects every step-th decoded frame and converts it to RGB24 at the
// size ffprobe reported, with the area scaler, BT.601 coefficients and
// limited range whatever the stream's colour tags say. The fixed size keeps
// the raw stream framed if the video changes resolution.
func filter(st stream, step int) string {
	return fmt.Sprintf("select='not(mod(n\\,%d))',scale=w=%d:h=%d:flags=area:in_color_matrix=bt601:in_range=tv,format=rgb24",
		step, st.width, st.height)
}

// decodeArgs make frame n the n-th decoded frame of the video stream spec
// selects, in its stored orientation and codec cropping, and fail on any
// decoding error. threads replaces ffmpeg's default, which ignores a CPU quota.
func decodeArgs(input, spec string, st stream, step, threads int) []string {
	t := strconv.Itoa(threads)
	args := []string{"-nostdin", "-v", "error", "-max_error_rate", "0", "-threads", t, "-filter_threads", t}
	args = append(args, inputArgs...)
	args = append(args, "-noautorotate", "-apply_cropping", "codec", "-reinit_filter", "0", "-i", input,
		"-map", "0:"+spec, "-fps_mode", "passthrough", "-vf", filter(st, step),
		"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1")
	return args
}

type job struct {
	index int
	frame *[]byte
	done  chan result
}

type result struct {
	hash *pdq.Hash
	err  error
}

func decode(parent context.Context, ffmpeg, input, spec string, st stream, step, threads, workers int) ([]Feature, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	stderr := &capped{max: maxStderr}
	cmd := exec.CommandContext(ctx, ffmpeg, decodeArgs(input, spec, st, step, threads)...)
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("vpdq: ffmpeg stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("vpdq: start ffmpeg: %w", err)
	}

	size := st.width * st.height * 3
	frames := sync.Pool{New: func() any { b := make([]byte, size); return &b }}

	// A frame goes to a worker before it is queued for output, so a frame the
	// collector waits on has always reached a worker.
	jobs := make(chan job)
	order := make(chan job, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for j := range jobs {
				h, err := pdq.ComputeHashFromRGB24(*j.frame, st.height, st.width, pdq.CPPLuma())
				frames.Put(j.frame)
				j.done <- result{h, err}
			}
		})
	}

	readErr := make(chan error, 1)
	go func() {
		defer close(order)
		defer close(jobs)
		for i := 0; ; i++ {
			frame, ok := frames.Get().(*[]byte)
			if !ok {
				readErr <- errors.New("vpdq: frame pool returned a foreign value")
				return
			}
			if _, err := io.ReadFull(stdout, *frame); err != nil {
				if errors.Is(err, io.EOF) {
					readErr <- nil
				} else {
					readErr <- fmt.Errorf("vpdq: read frame %d: %w", i, err)
				}
				return
			}
			j := job{index: i, frame: frame, done: make(chan result, 1)}
			select {
			case jobs <- j:
			case <-ctx.Done():
				readErr <- ctx.Err()
				return
			}
			select {
			case order <- j:
			case <-ctx.Done():
				readErr <- ctx.Err()
				return
			}
		}
	}()

	var features []Feature
	var hashErr error
	for j := range order {
		res := <-j.done
		if res.err != nil {
			if hashErr == nil {
				hashErr = fmt.Errorf("vpdq: hash frame %d: %w", j.index*step, res.err)
				cancel()
			}
			continue
		}
		n := j.index * step
		features = append(features, Feature{
			Hash:        res.hash.Data,
			FrameNumber: n,
			Quality:     res.hash.Quality,
			Timestamp:   float32(n) / float32(st.fps),
		})
	}
	wg.Wait()
	rerr := <-readErr
	werr := cmd.Wait()

	switch {
	case hashErr != nil:
		return nil, hashErr
	case parent.Err() != nil:
		return nil, parent.Err()
	case werr != nil:
		return nil, fmt.Errorf("vpdq: ffmpeg %s: %w: %s", input, werr, stderr)
	case rerr != nil:
		return nil, rerr
	}
	return features, nil
}

// capped keeps the first max bytes written to it and counts the rest.
type capped struct {
	max     int
	buf     []byte
	dropped int
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.max - len(c.buf)
	if room >= len(p) {
		c.buf = append(c.buf, p...)
	} else {
		c.buf = append(c.buf, p[:max(room, 0)]...)
		c.dropped += len(p) - max(room, 0)
	}
	return len(p), nil
}

func (c *capped) String() string {
	s := strings.TrimSpace(string(c.buf))
	if c.dropped > 0 {
		s += fmt.Sprintf(" [%d more bytes]", c.dropped)
	}
	return s
}
