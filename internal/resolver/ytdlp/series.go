package ytdlp

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/junkerderprovinz/knightloader/internal/filemode"
	"github.com/junkerderprovinz/knightloader/internal/httpx"
	"github.com/junkerderprovinz/knightloader/internal/nowindow"
	"github.com/junkerderprovinz/knightloader/internal/rules"
)

// Series files a video row as an episode of a show, in the layout Jellyfin,
// Kodi and Plex read without a scraper:
//
//	<Show>/Season <YYYY>/<Show> - S<YYYY>E<MMDD><nn> - <Title>.<ext>
//
// The season is the year of the upload and the episode its day, with nn
// counting the uploads of that day. The show's folder gets a tvshow.nfo and
// the channel's avatar and banner as poster and fanart, and every episode an
// episodedetails NFO beside it.
type Series struct {
	// Show names the show's folder and starts every file name. Empty takes
	// the channel's name.
	Show string `json:"show"`
}

func (s Series) sanitize() Series {
	s.Show = clip(strings.TrimSpace(s.Show))
	return s
}

// seriesTemplate is the -o template of a series row. finish puts the day's
// counter into the episode number once the file is there, since yt-dlp
// cannot know how many uploads of the day the season already holds.
func seriesTemplate(s Series) string {
	show := "%(channel,uploader,extractor)s"
	if s.Show != "" {
		show = strings.ReplaceAll(rules.FileSegment(s.Show), "%", "%%")
	}
	return show + "/Season %(upload_date>%Y|0000)s/" +
		show + " - S%(upload_date>%Y|0000)sE%(upload_date>%m%d|0000)s - %(title)s.%(ext)s"
}

// seriesTimeout bounds asking for the channel's page and fetching its art.
const seriesTimeout = time.Minute

// maxArtBytes bounds one picture of a channel.
const maxArtBytes = 20 << 20

// placeEpisode gives the file yt-dlp wrote under seriesTemplate its episode
// number, writes its NFO and, the first time, the show's, and returns where
// the file is. A file whose name does not have the template's shape stays
// where it is.
func (b *Backend) placeEpisode(s Series, file string, d infoDict) string {
	showDir := filepath.Dir(filepath.Dir(file))
	show := filepath.Base(showDir)
	// rest is "<YYYY>E<MMDD> - <title>.<ext>".
	rest, ok := strings.CutPrefix(filepath.Base(file), show+" - S")
	if !ok || len(rest) < 12 || rest[4] != 'E' || rest[9:12] != " - " {
		return file
	}
	day, ok := uploadDay(d.UploadDate)
	if !ok {
		day = time.Now()
	}
	year, mmdd := day.Format("2006"), day.Format("0102")
	season := filepath.Join(showDir, "Season "+year)
	stem := show + " - S" + year + "E" + mmdd
	n := episodeNumber(season, stem, d.ID)
	to := filepath.Join(season, fmt.Sprintf("%s%02d - %s", stem, n, rest[12:]))
	if err := os.MkdirAll(season, filemode.Dir); err != nil {
		return file
	}
	if err := os.Rename(file, to); err != nil {
		return file
	}
	// Only gone once empty, which it is when yt-dlp had no date for it.
	_ = os.Remove(filepath.Dir(file))

	title := firstNonEmpty(s.Show, d.Channel, d.Uploader, show)
	_ = writeXML(nfoPath(to), nfoEpisode{
		Title:     firstNonEmpty(d.Title, d.FullTitle),
		ShowTitle: title,
		Season:    day.Year(),
		Episode:   (int(day.Month())*100+day.Day())*100 + n,
		Aired:     day.Format("2006-01-02"),
		Plot:      strings.TrimSpace(d.Description),
		UniqueID:  sourceID(d.Extractor, d.ID),
	})
	b.placeShow(showDir, title, d)
	return to
}

// uploadDay reads yt-dlp's YYYYMMDD.
func uploadDay(s string) (time.Time, bool) {
	t, err := time.Parse("20060102", strings.TrimSpace(s))
	return t, err == nil
}

// episodeNumber is the counter the upload id gets among the episodes of one
// day: the one it already has when it was fetched before, else the next one.
func episodeNumber(season, stem, id string) int {
	entries, err := os.ReadDir(season)
	if err != nil {
		return 1
	}
	day := regexp.MustCompile(`^` + regexp.QuoteMeta(stem) + `(\d{2,}) - `)
	highest := 0
	for _, e := range entries {
		m := day.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if strings.EqualFold(filepath.Ext(e.Name()), ".nfo") && id != "" && nfoNamesID(filepath.Join(season, e.Name()), id) {
			return n
		}
		highest = max(highest, n)
	}
	return highest + 1
}

func nfoNamesID(path, id string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		IDs []string `xml:"uniqueid"`
	}
	if xml.Unmarshal(b, &doc) != nil {
		return false
	}
	for _, x := range doc.IDs {
		if strings.TrimSpace(x) == id {
			return true
		}
	}
	return false
}

// placeShow writes the show's tvshow.nfo and art unless the folder has its
// NFO already, which may be somebody's own.
func (b *Backend) placeShow(showDir, title string, d infoDict) {
	nfo := filepath.Join(showDir, "tvshow.nfo")
	if _, err := os.Stat(nfo); !errors.Is(err, fs.ErrNotExist) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), seriesTimeout)
	defer cancel()
	page := b.channelPage(ctx, d.ChannelURL)
	id := firstNonEmpty(page.ID, d.ChannelID)
	_ = writeXML(nfo, nfoShow{Title: title, Plot: strings.TrimSpace(page.Description), UniqueID: sourceID(d.Extractor, id)})
	// The site names the pictures, and a public site must not steer a fetch
	// into the LAN.
	if u, err := url.Parse(d.ChannelURL); err == nil {
		ctx = httpx.Confine(ctx, httpx.ScopeOfHost(ctx, u.Hostname()))
	}
	for _, th := range page.Thumbnails {
		switch th.ID {
		case "avatar_uncropped":
			b.fetchArt(ctx, th.URL, filepath.Join(showDir, "poster"))
		case "banner_uncropped":
			b.fetchArt(ctx, th.URL, filepath.Join(showDir, "fanart"))
		}
	}
}

// channel is the part of a channel's page this file reads. The avatar and
// banner are in it, a video's own info dict has neither.
type channel struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Thumbnails  []struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	} `json:"thumbnails"`
}

// channelPage asks yt-dlp for the channel at rawurl without its videos. A site
// without channel pages, or one that fails, leaves it empty.
func (b *Backend) channelPage(ctx context.Context, rawurl string) channel {
	if rawurl == "" {
		return channel{}
	}
	args := []string{"--no-warnings", "-J", "--flat-playlist", "--playlist-items", "0"}
	args = append(args, b.proxyArgs()...)
	args = append(args, b.denoArgs()...)
	cmd := nowindow.CommandContext(ctx, b.binary(), append(args, rawurl)...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	out, err := cmd.Output()
	var c channel
	if err != nil || json.Unmarshal(out, &c) != nil {
		return channel{}
	}
	return c
}

// fetchArt saves the picture at rawurl as stem plus the extension of what the
// site served, jpg or png, which all three readers take. Anything else, a
// failure and a picture already there leave it be.
func (b *Backend) fetchArt(ctx context.Context, rawurl, stem string) {
	for _, ext := range []string{".jpg", ".png"} {
		if _, err := os.Stat(stem + ext); err == nil {
			return
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArtBytes))
	if err != nil {
		return
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[http.DetectContentType(body)]
	if ext == "" {
		return
	}
	_ = os.WriteFile(stem+ext, body, filemode.File)
}

// nfoEpisode and nfoShow are Kodi's episodedetails and tvshow documents, which
// Jellyfin and Plex's local agents read too.
type nfoEpisode struct {
	XMLName   xml.Name  `xml:"episodedetails"`
	Title     string    `xml:"title,omitempty"`
	ShowTitle string    `xml:"showtitle,omitempty"`
	Season    int       `xml:"season"`
	Episode   int       `xml:"episode"`
	Aired     string    `xml:"aired,omitempty"`
	Plot      string    `xml:"plot,omitempty"`
	UniqueID  *uniqueID `xml:"uniqueid,omitempty"`
}

type nfoShow struct {
	XMLName  xml.Name  `xml:"tvshow"`
	Title    string    `xml:"title,omitempty"`
	Plot     string    `xml:"plot,omitempty"`
	UniqueID *uniqueID `xml:"uniqueid,omitempty"`
}

// sourceID is the uniqueid of id at the site extractor names, or nil for no
// id. Kodi treats the type as an opaque namespace, and extractor_key names the
// same source the same way every time.
func sourceID(extractor, id string) *uniqueID {
	if id == "" {
		return nil
	}
	kind := strings.ToLower(strings.TrimSpace(extractor))
	if kind == "" {
		kind = "ytdlp"
	}
	return &uniqueID{Type: kind, Default: true, Value: id}
}
