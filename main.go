package main

import (
	"flag"
	"fmt"
	"github.com/mikkyang/id3-go"
	"github.com/mmcdole/gofeed"
	"io"
	"log"
	"net/http"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	GlsFeed = "https://golangshow.com/index.xml"
	ps      = string(os.PathSeparator)
)

var (
	threads = flag.Int("t", 4, "Threads to simultaneously download media files.")
)

// Main struct
type Glsdl struct {
	source       *io.ReadCloser
	threads      int
	waitGroup    sync.WaitGroup
	parsePattern *regexp.Regexp
	downloadDir  string
	statDl       int
	statProcess  int
	statFail     int
	statTime     time.Duration
}

// The constructor.
// Takes source of a feed and maximum number of threads.
func NewGlsdl(source *io.ReadCloser, threads int) *Glsdl {
	usr, _ := user.Current()
	dl := Glsdl{
		source:       source,
		threads:      threads,
		parsePattern: regexp.MustCompile(`^[Выпуск|Episode]+\s+([[:alnum:]]+)\.*\s*(.*?)$`),
		downloadDir:  strings.Join([]string{usr.HomeDir, "Music", "Podcast", "GolangShow"}, ps),
		statDl:       0,
		statProcess:  0,
		statFail:     0,
	}

	if _, err := os.Stat(dl.downloadDir); os.IsNotExist(err) {
		_ = os.MkdirAll(dl.downloadDir, 0755)
	}

	return &dl
}

// Main func to start the download process.
func (dl *Glsdl) Process() {
	start := time.Now()

	// Parse the feed.
	parser := gofeed.NewParser()
	feed, err := parser.Parse(*dl.source)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Progress:")

	// Download the comver.
	dl.waitGroup.Add(1)
	go func() {
		defer dl.waitGroup.Done()
		filename := dl.downloadDir + ps + "cover.png"
		if err := dl.downloadFile(feed.Image.URL, filename); err != nil {
			log.Println(err)
		}
		fmt.Println("* cover file")
		dl.statProcess++
	}()

	// Split feed to chunks according threads number param and process them simultaneously.
	counter := 0
	for _, item := range feed.Items {
		counter++
		dl.waitGroup.Add(1)
		go dl.worker(item)
		if counter >= dl.threads {
			dl.waitGroup.Wait()
			counter = 0
		}
	}
	if counter > 0 {
		dl.waitGroup.Wait()
	}

	dl.statTime = time.Since(start)
}

// Build the statistics report.
func (dl *Glsdl) Report() (report []string) {
	report = make([]string, 0)
	report = append(report, fmt.Sprintf("* %d files were downloaded", dl.statDl))
	report = append(report, fmt.Sprintf("* %d files were processes", dl.statProcess))
	report = append(report, fmt.Sprintf("* %d files were failed", dl.statFail))
	report = append(report, fmt.Sprintf("* %s spent", dl.statTime))

	return
}

// Worker func. Takes feed item as param, download its media file and complete it with th ID3 tags.
func (dl *Glsdl) worker(item *gofeed.Item) {
	defer dl.waitGroup.Done()
	if len(item.Enclosures[0].Length) == 0 {
		return
	}

	// Parse the title.
	prefix, title := dl.parseTitle(item)
	finalTitle := "[" + prefix + "] " + title

	opts := make([]string, 0)

	// Compose output filename and download it if needed.
	filename := dl.downloadDir + ps + prefix + " - " + title + ".mp3"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		opts = append(opts, "dl")
		err := dl.downloadFile(item.Enclosures[0].URL, filename)
		if err != nil {
			log.Println(err)
			dl.statFail++
			return
		}
	}

	// Open media file and complete it with ID3 tags.
	tag, err := id3.Open(filename)
	if err != nil {
		log.Println(err)
		dl.statFail++
		return
	}
	published, _ := time.Parse(time.RFC1123Z, item.Published)
	tag.SetTitle(finalTitle)
	tag.SetArtist(item.Author.Name)
	tag.SetAlbum("GolangShow")
	tag.SetGenre("Technology")
	tag.SetYear(strconv.Itoa(published.Year()))
	defer func() {
		_ = tag.Close()
	}()

	dl.statProcess++
	opts = append(opts, "id3")

	fmt.Println("*", finalTitle, "["+strings.Join(opts, "+")+"]")
}

// Parse the title of item and split it to the number and title.
func (dl *Glsdl) parseTitle(item *gofeed.Item) (prefix, title string) {
	res := dl.parsePattern.FindStringSubmatch(item.Title)
	if len(res) == 0 {
		return "", item.Title
	}
	prefix, title = res[1], res[2]
	if len(title) == 0 {
		title = item.Author.Name
	}
	title = strings.Replace(title, ps, "_", -1)
	return
}


// Download the file and report about any error.
func (dl *Glsdl) downloadFile(url, dest string) (err error) {
	fh, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		err := fh.Close()
		if err != nil {
			log.Println(err)
		}
	}()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			log.Println(err)
		}
	}()

	_, err = io.Copy(fh, resp.Body)
	if err != nil {
		return err
	}

	dl.statDl++

	return nil
}

func main() {
	flag.Parse()

	// Download the feed.
	source, err := http.Get(GlsFeed)
	if err != nil {
		log.Println(err)
	}

	// Process feed.
	dl := NewGlsdl(&source.Body, *threads)
	dl.Process()

	// Display statistics.
	fmt.Println("Statistics:")
	fmt.Println(strings.Join(dl.Report(), "\n"))
}
