package main

import (
	"flag"
	"net/http"
	"log"
	"fmt"
	"net/http/cookiejar"
	"io/ioutil"
	"encoding/json"
	"os"
	"path"
	id3 "github.com/mmorton/id3-go"
	id3v2 "github.com/mmorton/id3-go/v2"
	query "github.com/hailiang/html-query"
	queryEx "github.com/hailiang/html-query/expr"
	"path/filepath"
	"io"
	"strconv"
	"html"
	"strings"
)

type (
	Track struct {
		Guid string		`json:guid`
		Title string	`json:title`
		Mp3 string		`json:mp3`
		Oga string		`json:oga`
		Number int
	}

	TrackSet struct {
		Tracks map[string]Track		`json:Tracks`
		Success bool				`json:Success`
	}

	Album struct {
		Title string
		TrackSet *TrackSet
		TrackIndex map[string]int
	}
)

const (
	HOST string = "sndtst.com"
)

func main() {
	slug := flag.String("slug", "", "")
	dest := flag.String("dest", "", "")

	flag.Parse();

	if *slug == "" {
		log.Fatalf("--slug is required")
	}

	if *dest == "" {
		log.Fatalf("--dest is required")
	}

	Download(*slug, *dest)
}

func sanitize(text string) string {
	s := text
	s = strings.Replace(s, "<", "-", -1)
	s = strings.Replace(s, ">", "-", -1)
	s = strings.Replace(s, ":", "-", -1)
	s = strings.Replace(s, "\"", "-", -1)
	s = strings.Replace(s, "/", "-", -1)
	s = strings.Replace(s, "\\", "-", -1)
	s = strings.Replace(s, "|", "-", -1)
	s = strings.Replace(s, "?", "-", -1)
	s = strings.Replace(s, "*", "-", -1)
	return s
}

func Download(slug string, dest string) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
	}

	album, err := GetAlbum(client, slug)
	if err != nil {
		log.Fatalf("Could not get album: %#v", err)
	}

	albumTitle := html.UnescapeString(album.Title)
	albumPath := filepath.Join(dest, sanitize(albumTitle))
	err = os.MkdirAll(albumPath, os.ModePerm)
	if err != nil {
		log.Fatalf("Could not make dir for album: %s, err: %#v", albumPath, err)
	}

	type res struct {
		track Track
		path string
		err error
	}

	ch := make(chan res)

	for _, track := range album.TrackSet.Tracks {
		go func(track Track) {
			log.Printf("Fetching: %s", track.Title)
			resp, err := client.Get(fmt.Sprintf("http://%s%s", HOST, track.Mp3))
			if err != nil {
				ch <- res{track:track, err:err}
				return
			}
			defer resp.Body.Close()

			trackNumText := strconv.Itoa(album.TrackIndex[track.Guid])
			trackTitle := html.UnescapeString(track.Title)
			trackPath := path.Join(albumPath, sanitize(fmt.Sprintf("%s - %s.mp3", trackNumText, trackTitle)))

			out, err := os.Create(trackPath)
			if err != nil {
				ch <- res{track:track, err:err}
				return
			}

			_, err = io.Copy(out, resp.Body)
			if err != nil {
				out.Close()
				ch <- res{track:track, err:err}
				return
			}
			out.Close()

			mp3, err := id3.Open(trackPath)
			if err != nil {
				ch <- res{track:track, err:err}
				return
			}

			mp3.SetArtist("SNDTST")
			mp3.SetAlbum(album.Title)
			mp3.SetTitle(trackTitle)
			trackNumFrame := id3v2.NewTextFrame(id3v2.V23FrameTypeMap["TRCK"], trackNumText)
			mp3.AddFrames(trackNumFrame)
			mp3.Close()

			ch <- res{track:track, err:nil}
		}(track)
	}

	for i := 0; i < len(album.TrackSet.Tracks); i++  {
		r := <- ch
		if r.err != nil {
			log.Printf("Fetch error: %s, err: %#v", r.track.Title, r.err)
	 	} else {
			log.Printf("Fetched: %s", r.track.Title)
		}
	}
}

func GetAlbum(client *http.Client, slug string) (*Album, error) {
	resp, err := client.Get(fmt.Sprintf("http://%s/%s", HOST, slug))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	document, err := query.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	album := Album{}
	album.Title = *document.H1().Text()

	trackCounter := 1
	trackIndex := map[string]int{}
	document.Ol(queryEx.Id("Playlist")).Children(queryEx.Li).For(func(item *query.Node) {
		trackIndex[*item.Attr("data-song")] = trackCounter
		trackCounter += 1
	})

	trackSet, err := GetTrackSet(client, slug)
	if err != nil {
		return nil, err
	}

	album.TrackSet = trackSet
	album.TrackIndex = trackIndex

	return &album, nil
}

func GetTrackSet(client *http.Client, slug string) (*TrackSet, error) {
	resp, err := client.Get(fmt.Sprintf("http://%s/%s.json", HOST, slug))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var trackSet TrackSet
	err = json.Unmarshal(body, &trackSet)
	if err != nil {
		return nil, err
	}

	return &trackSet, nil
}
