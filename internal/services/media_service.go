package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// getYouTubeTitle fetches YouTube video title via OEmbed
func getYouTubeTitle(videoURL string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://www.youtube.com/oembed?url=%s&format=json", url.QueryEscape(videoURL)))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var data struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	return data.Title
}

// getTikTokTitle fetches TikTok video title via OEmbed
func getTikTokTitle(videoURL string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("https://www.tiktok.com/oembed?url=%s&format=json", url.QueryEscape(videoURL)))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var data struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	return strings.TrimSpace(data.Title)
}

type MediaService struct{}

func NewMediaService() *MediaService {
	return &MediaService{}
}

// ResolveURL checks if a URL is from a known platform and returns a suggested title
func (s *MediaService) ResolveURL(rawURL string) (string, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL format")
	}

	host := strings.ToLower(u.Host)
	path := u.Path
	fileName := "Imported Media Link"

	pathParts := strings.Split(path, "/")
	if len(pathParts) > 0 {
		lastPart := pathParts[len(pathParts)-1]
		if lastPart != "" && strings.Contains(lastPart, ".") {
			fileName = lastPart
		}
	}

	if strings.Contains(host, "youtube.com") || strings.Contains(host, "youtu.be") {
		title := getYouTubeTitle(rawURL)
		if title != "" {
			fileName = title
		} else {
			videoID := ""
			if strings.Contains(host, "youtu.be") {
				videoID = strings.TrimPrefix(path, "/")
			} else {
				videoID = u.Query().Get("v")
			}
			if videoID != "" {
				fileName = fmt.Sprintf("YouTube - %s", videoID)
			} else {
				fileName = "YouTube Audio"
			}
		}
		return rawURL, fileName, nil
	}

	if strings.Contains(host, "vimeo.com") {
		vimeoID := strings.TrimPrefix(path, "/")
		if vimeoID != "" {
			fileName = fmt.Sprintf("Vimeo - %s", vimeoID)
		}
		return rawURL, fileName, nil
	}

	if strings.Contains(host, "drive.google.com") {
		videoID := ""
		if strings.Contains(path, "/file/d/") {
			parts := strings.Split(path, "/")
			for i, p := range parts {
				if p == "d" && i+1 < len(parts) {
					videoID = parts[i+1]
					break
				}
			}
		} else {
			videoID = u.Query().Get("id")
		}

		if videoID != "" {
			resolved := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", videoID)
			return resolved, "Google Drive File", nil
		}
	}

	if strings.Contains(host, "tiktok.com") {
		title := getTikTokTitle(rawURL)
		if title != "" {
			fileName = title
		} else {
			fileName = "TikTok Video"
		}
		return rawURL, fileName, nil
	}

	if strings.Contains(host, "dropbox.com") {
		resolved := rawURL
		if strings.Contains(rawURL, "dl=0") {
			resolved = strings.Replace(rawURL, "dl=0", "dl=1", 1)
		}
		if fileName == "Imported Media Link" {
			fileName = "Dropbox File"
		}
		return resolved, fileName, nil
	}

	return rawURL, fileName, nil
}

func isSafeURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	host := u.Hostname()
	if host == "" {
		return false
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}

	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return false
		}
		if ip4 := ip.To4(); ip4 != nil {
			if ip4[0] == 10 {
				return false
			}
			if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
				return false
			}
			if ip4[0] == 192 && ip4[1] == 168 {
				return false
			}
		} else {
			if len(ip) == 16 && (ip[0] == 0xfc || ip[0] == 0xfd) {
				return false
			}
		}
	}

	return true
}

func isDirectMediaExtension(rawPath string) bool {
	ext := strings.ToLower(filepath.Ext(rawPath))
	switch ext {
	case ".mp3", ".wav", ".m4a", ".flac", ".aac", ".ogg", ".oga", ".opus", ".wma",
		".mp4", ".mov", ".mkv", ".webm", ".avi", ".m4v", ".3gp", ".ts":
		return true
	default:
		return false
	}
}

func isKnownPlatformURL(host string) bool {
	platforms := []string{
		"youtube.com", "youtu.be",
		"tiktok.com",
		"vimeo.com",
		"facebook.com", "fb.watch", "fb.com",
		"instagram.com",
		"twitter.com", "x.com", "t.co",
		"reddit.com", "redd.it", "v.redd.it",
		"soundcloud.com",
		"dailymotion.com", "dai.ly",
		"dropbox.com",
	}
	for _, p := range platforms {
		if strings.Contains(host, p) {
			return true
		}
	}
	return false
}

func (s *MediaService) downloadWithYtDlp(ctx context.Context, host, rawURL, outputBase string) (string, error) {
	binPath, err := exec.LookPath("yt-dlp")
	if err != nil {
		binPath = "/usr/local/bin/yt-dlp"
	}

	downloadBase := outputBase + "_raw"
	downloadTemplate := downloadBase + ".%(ext)s"
	baseArgs := []string{
		"-f", "ba/b/bestaudio/best",
		"--no-playlist",
		"--restrict-filenames",
		"--force-ipv4",
		"--no-check-certificates",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36",
		"-o", downloadTemplate,
		rawURL,
	}

	var foundCookiePath string
	var cookiePaths []string
	if envPath := os.Getenv("YOUTUBE_COOKIES_PATH"); envPath != "" {
		cookiePaths = append(cookiePaths, envPath)
	}
	cookiePaths = append(cookiePaths,
		"/var/www/lexscripts-backend-staging/cookies.txt",
		"/var/www/lexscripts-backend-prod/cookies.txt",
		"/root/cookies.txt",
		"./cookies.txt",
		"cookies.txt",
	)

	for _, cp := range cookiePaths {
		if _, err := os.Stat(cp); err == nil {
			foundCookiePath = cp
			break
		}
	}

	runYtDlp := func(useCookies bool, playerClient string) (string, error) {
		args := make([]string, len(baseArgs))
		copy(args, baseArgs)

		if strings.Contains(host, "youtube.com") || strings.Contains(host, "youtu.be") {
			if playerClient != "" {
				args = append(args, "--extractor-args", "youtube:player_client="+playerClient)
			}
		}

		if useCookies && foundCookiePath != "" {
			args = append([]string{"--cookies", foundCookiePath}, args...)
		}

		cmd := exec.CommandContext(ctx, binPath, args...)
		out, err := cmd.CombinedOutput()
		outStr := string(out)
		if err != nil {
			return outStr, err
		}

		matches, _ := filepath.Glob(downloadBase + ".*")
		for _, m := range matches {
			if !strings.HasSuffix(m, ".part") && !strings.HasSuffix(m, ".ytdl") {
				return m, nil
			}
		}
		return outStr, fmt.Errorf("media output file not found")
	}

	// Attempt 1: Standard yt-dlp execution (uses Deno JS solver natively)
	res1, err1 := runYtDlp(false, "")
	if err1 == nil && !strings.HasPrefix(res1, "media output file not found") {
		return res1, nil
	}

	log.Printf("[MediaService] Standard Deno attempt failed for %s: %v. Retrying with cookies...", rawURL, err1)

	// Attempt 2: With cookies (if available, for age-gated videos)
	if foundCookiePath != "" {
		res2, err2 := runYtDlp(true, "")
		if err2 == nil && !strings.HasPrefix(res2, "media output file not found") {
			return res2, nil
		}
		log.Printf("[MediaService] Cookie attempt failed for %s: %v", rawURL, err2)
	}

	// Attempt 3: Web Embedded & VisionOS fallback client
	res3, err3 := runYtDlp(false, "web_embedded,visionos")
	if err3 == nil && !strings.HasPrefix(res3, "media output file not found") {
		return res3, nil
	}

	log.Printf("[MediaService] All yt-dlp attempts failed for %s. Output: %s", rawURL, res3)
	return "", fmt.Errorf("yt-dlp download failed: %v", err3)
}

func (s *MediaService) downloadDirectHTTP(rawURL, outputBase string) (string, error) {
	rawDownloadPath := outputBase + ".ext"
	resp, err := http.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to download from link: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download failed with HTTP status %d", resp.StatusCode)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "text/plain") {
		return "", fmt.Errorf("provided URL returned a webpage instead of direct audio/video")
	}

	const maxDownloadSize = 500 * 1024 * 1024
	if resp.ContentLength > maxDownloadSize {
		return "", fmt.Errorf("file size exceeds 500MB limit")
	}

	out, err := os.Create(rawDownloadPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	limitedReader := io.LimitReader(resp.Body, maxDownloadSize+1)
	written, err := io.Copy(out, limitedReader)
	if err != nil {
		_ = os.Remove(rawDownloadPath)
		return "", err
	}

	if written > maxDownloadSize {
		_ = os.Remove(rawDownloadPath)
		return "", fmt.Errorf("file size exceeds 500MB limit")
	}

	return rawDownloadPath, nil
}

// DownloadAndConvert downloads a public link and converts it to a 16kHz mono WAV file
func (s *MediaService) DownloadAndConvert(rawURL string) (string, error) {
	if !isSafeURL(rawURL) {
		return "", fmt.Errorf("forbidden: local or private IP addresses cannot be accessed")
	}

	tempDir := "temp_media"
	_ = os.MkdirAll(tempDir, 0755)

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %v", err)
	}
	host := strings.ToLower(u.Host)

	uniqueID := fmt.Sprintf("media_%d_%d", os.Getpid(), time.Now().UnixNano())
	outputBase := filepath.Join(tempDir, uniqueID)
	finalPath := outputBase + ".wav"

	isCloudStorage := strings.Contains(host, "drive.google.com") || strings.Contains(host, "dropbox.com") || strings.Contains(host, "storage.googleapis.com") || strings.Contains(host, "s3.amazonaws.com")
	useYtDlp := isKnownPlatformURL(host) || (!isDirectMediaExtension(u.Path) && !isCloudStorage)

	var downloadPath string
	if useYtDlp {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		dlPath, dlErr := s.downloadWithYtDlp(ctx, host, rawURL, outputBase)
		if dlErr != nil {
			if !isKnownPlatformURL(host) {
				log.Printf("[MediaService] yt-dlp failed for %s, trying direct HTTP fallback: %v", rawURL, dlErr)
				dlPath, dlErr = s.downloadDirectHTTP(rawURL, outputBase)
			}
		}
		if dlErr != nil {
			return "", fmt.Errorf("failed to download media from link: %v", dlErr)
		}
		downloadPath = dlPath
	} else {
		dlPath, dlErr := s.downloadDirectHTTP(rawURL, outputBase)
		if dlErr != nil {
			return "", dlErr
		}
		downloadPath = dlPath
	}

	defer os.Remove(downloadPath)

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		ffmpegPath = "/usr/bin/ffmpeg"
	}

	ctxFFmpeg, cancelFFmpeg := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelFFmpeg()

	cmd := exec.CommandContext(ctxFFmpeg, ffmpegPath,
		"-i", downloadPath,
		"-vn",
		"-ar", "16000",
		"-ac", "1",
		"-f", "wav",
		"-y",
		finalPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[MediaService] ffmpeg error: %v\nOutput: %s", err, string(output))
		return "", fmt.Errorf("audio conversion failed: %v", err)
	}

	return finalPath, nil
}

func (s *MediaService) GetDuration(path string) (float64, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		ffprobePath = "/usr/bin/ffprobe"
	}

	cmd := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe failed: %v", err)
	}

	var duration float64
	_, err = fmt.Sscanf(strings.TrimSpace(string(output)), "%f", &duration)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %v", err)
	}

	return duration, nil
}
