package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/kardianos/osext"
	"golang.org/x/net/proxy"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
)

type AppArgs struct {
	PackageId  int
	PNG        bool
	APNG       bool
	GIF        bool
	BaseDir    string
	FolderName string
	Proxy      string
	Package    *StickerPackage
}

type StickerPrice struct {
	Country  string
	Currency string
	Symbol   string
	Price    float32
}

type Sticker struct {
	Id     int
	Width  int
	Height int
}

type StickerPackage struct {
	PackageId           int
	OnSale              bool
	ValidDays           int
	Title               map[string]string
	Author              map[string]string
	Price               []StickerPrice
	Stickers            []Sticker
	HasAnimation        bool
	HasSound            bool
	StickerResourceType string
}

var InvalidFileNameChars = [...]string{
	"\"", "<", ">", "|", "\x00", "\u0001", "\u0002", "\u0003",
	"\u0004", "\u0005", "\u0006", "\a", "\b", "\t", "\n", "\v",
	"\f", "\r", "\u000e", "\u000f", "\u0010", "\u0011", "\u0012",
	"\u0013", "\u0014", "\u0015", "\u0016", "\u0017", "\u0018",
	"\u0019", "\u001a", "\u001b", "\u001c", "\u001d", "\u001e",
	"\u001f", ":", "*", "?", "\\", "/",
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func getJson(url string, target interface{}) error {
	r, err := HttpClient.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(target)
}

func normalizeFileName(str string) string {
	for _, char := range InvalidFileNameChars {
		str = strings.Replace(str, char, "_", -1)
	}
	return strings.TrimSpace(str)
}

func Download(url, path string) error {
	output, err := os.Create(path)
	if err != nil {
		return err
	}
	defer output.Close()
	resp, err := HttpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(output, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

var InvalidPNGError = errors.New("Invalid PNG file")

func LoopAPNG(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	var head = make([]byte, 8)
	_, err = file.Read(head)
	if err != nil {
		return err
	}
	if bytes.Compare(
		head,
		[]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
	) != 0 {
		return InvalidPNGError
	}
	var dataSize int32
	var word = make([]byte, 4)
	var chunkType = make([]byte, 4)
	for {
		_, err = file.Read(word)
		if err != nil {
			return err
		}
		buf := bytes.NewBuffer(word)
		binary.Read(buf, binary.BigEndian, &dataSize)

		_, err = file.Read(chunkType)
		if err != nil {
			return err
		}

		if bytes.Compare(chunkType, []byte("acTL")) == 0 {
			_, err = file.Seek(4, 1)
			if err != nil {
				return err
			}
			_, err = file.Write([]byte{0, 0, 0, 0})
			if err != nil {
				return err
			}
			return nil
		}

		_, err = file.Seek(int64(dataSize+4), 1)
		if err != nil {
			return err
		}
	}
}

var PWD string
var ExePath string
var HttpClient *http.Client
var appArgs *AppArgs

func init() {
	var err error
	PWD, err = os.Getwd()
	check(err)

	ExePath, err = osext.ExecutableFolder()
	check(err)

	envPATH := os.Getenv("PATH")
	if runtime.GOOS == "windows" {
		if envPATH == "" {
			err = os.Setenv("PATH", fmt.Sprintf("%s;%s", ExePath, PWD))
		} else {
			err = os.Setenv("PATH", fmt.Sprintf("%s;%s;%s", ExePath, PWD, envPATH))
		}
	} else {
		if envPATH == "" {
			err = os.Setenv("PATH", fmt.Sprintf("%s:%s", ExePath, PWD))
		} else {
			err = os.Setenv("PATH", fmt.Sprintf("%s:%s:%s", ExePath, PWD, envPATH))
		}
	}
	check(err)

	appArgs = new(AppArgs)
	flag.IntVar(&appArgs.PackageId, "id", 0, "Sticker package ID")
	flag.BoolVar(&appArgs.PNG, "png", true, "Download static PNG")
	flag.BoolVar(&appArgs.APNG, "apng", true, "Download animated PNG (If available)")
	flag.BoolVar(&appArgs.GIF, "gif", true, "Convert animated PNG to GIF (If available)")
	flag.StringVar(&appArgs.BaseDir, "d", PWD, "Base directory for downloads")
	flag.StringVar(&appArgs.FolderName, "f", "", "Folder name to save the sticker package")
	flag.StringVar(&appArgs.Proxy, "proxy", "", "SOCKS5 proxy address (eg. 127.0.0.1:1080)")
	flag.Parse()

	if appArgs.GIF {
		if _, err := exec.LookPath("apng2gif"); err != nil {
			appArgs.GIF = false
			fmt.Fprintln(os.Stderr, `apng2gif is not found. GIF conversion will not be executed.
Please download the executable from https://sourceforge.net/projects/apng2gif/ and place it next to this program.`)
		}
	}

	httpTransport := &http.Transport{}
	HttpClient = &http.Client{
		Transport: httpTransport,
	}
	if appArgs.Proxy != "" {
		dailer, err := proxy.SOCKS5("tcp", appArgs.Proxy, nil, proxy.Direct)
		check(err)
		httpTransport.Dial = dailer.Dial
	}
}

func main() {
	stickerPackage := new(StickerPackage)
	err := getJson(fmt.Sprintf("http://dl.stickershop.line.naver.jp/products/0/0/1/%d/android/productInfo.meta", appArgs.PackageId), stickerPackage)
	check(err)

	appArgs.Package = stickerPackage
	appArgs.APNG = appArgs.APNG && stickerPackage.HasAnimation
	appArgs.GIF = appArgs.GIF && stickerPackage.HasAnimation

	var folderName string
	if appArgs.FolderName != "" {
		folderName = appArgs.FolderName
	} else {
		folderName = normalizeFileName(fmt.Sprintf("%d - %s", stickerPackage.PackageId, stickerPackage.Title["en"]))
	}
	fmt.Println(folderName)
	folderPath := path.Join(appArgs.BaseDir, folderName)

	if appArgs.PNG {
		dir := path.Join(folderPath, "PNG")
		os.MkdirAll(dir, 0755)
		for _, sticker := range stickerPackage.Stickers {
			fmt.Printf("[PNG] %d ... ", sticker.Id)
			url := fmt.Sprintf("http://sdl-stickershop.line.naver.jp/products/0/0/1/%d/android/stickers/%d.png", stickerPackage.PackageId, sticker.Id)
			filePath := path.Join(dir, fmt.Sprintf("%d.png", sticker.Id))
			err := Download(url, filePath)
			if err != nil {
				fmt.Println("Error")
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("Done")
			}
		}
	}

	if appArgs.APNG || appArgs.GIF {
		dir := path.Join(folderPath, "APNG")
		os.MkdirAll(dir, 0755)
		for _, sticker := range stickerPackage.Stickers {
			fmt.Printf("[APNG] %d ... ", sticker.Id)
			url := fmt.Sprintf("http://sdl-stickershop.line.naver.jp/products/0/0/1/%d/android/animation/%d.png", stickerPackage.PackageId, sticker.Id)
			filePath := path.Join(dir, fmt.Sprintf("%d.png", sticker.Id))
			err := Download(url, filePath)
			if err != nil {
				fmt.Println("Error")
				fmt.Fprintln(os.Stderr, err)
			} else {
				err := LoopAPNG(filePath)
				if err != nil && err != io.EOF {
					fmt.Println("Error")
					fmt.Fprintln(os.Stderr, err)
				} else {
					fmt.Println("Done")
				}
			}
		}
	}

	if appArgs.GIF {
		gifDir := path.Join(folderPath, "GIF")
		apngDir := path.Join(folderPath, "APNG")
		os.MkdirAll(gifDir, 0755)
		for _, sticker := range stickerPackage.Stickers {
			fmt.Printf("[GIF] %d ... ", sticker.Id)
			err := exec.Command(
				"apng2gif",
				path.Join(apngDir, fmt.Sprintf("%d.png", sticker.Id)),
				path.Join(gifDir, fmt.Sprintf("%d.gif", sticker.Id)),
			).Run()
			if err != nil {
				fmt.Println("Error")
				fmt.Fprintln(os.Stderr, err)
			} else {
				fmt.Println("Done")
			}
		}
	}

	if appArgs.GIF && !appArgs.APNG {
		fmt.Print("Removing APNG ... ")
		err := os.RemoveAll(path.Join(folderPath, "APNG"))
		check(err)
		fmt.Println("Done")
	}

	fmt.Print("Generating index.html ... ")
	t, err := template.New("index").Parse(IndexTemplate)
	check(err)
	indexFile, err := os.Create(path.Join(folderPath, "index.html"))
	check(err)
	defer indexFile.Close()
	t.Execute(indexFile, appArgs)
	fmt.Println("Done")
}

const IndexTemplate = `
<!DOCTYPE html>
<html lang="en">
    <head>
        <meta charset="utf-8">
            <title>
                {{index .Package.Title "en"}} - LINE Stickers
            </title>
            <link href="https://fonts.googleapis.com/css?family=Lato" rel="stylesheet" type="text/css">
                <style>
                    body, html {
                        padding: 0;
                        margin: 0;
                        background-color: #f5f5f5;
                        font-family: "Lato", "Lucida Grande","Lucida Sans Unicode", Tahoma, Sans-Serif;
                        color: #444;
                    }
                    h1 {
                        font-size: 2.2em;
                        margin-bottom: 0;
                    }
                    h4 {
                        font-size: 1.3em;
                        margin-top: 1em;
                        color: #555;
                    }
                    .content {
                        width: 720px;
                        margin: 0 auto;
                        text-align: center;
                    }
                    .content header {
                        padding: 1em 0;
                        text-align: left;
                    }
                    .flow {
                        width: 100%;
                        text-align: left;
                        display: none;
                        margin: 2em 0;
                    }
                    .sticker {
                        width: 24%;
                        display: inline-block;
                        margin: 10px 0;
                        transition: all 0.25s;
                        border-radius: 2px;
                    }
                    .sticker:hover {
                        box-shadow: 0px 0px 10px 0px rgba(0,0,0,0.15);
                    }
                    .sticker img {
                        width: 100%;
                        height: auto;
                    }
                    .tab {
                        display: none;
                    }
                    .tab + label {
                        padding: 0.5em 1em;
                        font-size: 1.2em;
                        border: 1px solid #d5d5d5;
                        border-radius: 7px;
                        margin: 0 3px;
                        cursor: pointer;
                    }
                    .tab:checked + label {
                        background-color: #444;
                        color: #f5f5f5;
                        border-color: transparent;
                    }
                    .tab:checked:nth-of-type(1) ~ .flow:nth-of-type(1) {
                        display: block;
                    }
                    .tab:checked:nth-of-type(2) ~ .flow:nth-of-type(2) {
                        display: block;
                    }
                    .tab:checked:nth-of-type(3) ~ .flow:nth-of-type(3) {
                        display: block;
                    }
                </style>
            </link>
        </meta>
    </head>
    <body>
        <div class="content">
            <header>
                <h1>
                    {{.Package.PackageId}} - {{index .Package.Title "en"}}
                </h1>
                <h4>
                    {{index .Package.Author "en"}}
                </h4>
            </header>

			{{if .APNG -}}
            <input type="radio" id="APNG" name="formatTab" class="tab" checked>
            <label for="APNG">APNG</label>
            {{- end}}

			{{if .GIF -}}
            <input type="radio" id="GIF" name="formatTab" class="tab" {{if not .APNG -}} checked {{- end}}>
            <label for="GIF">GIF</label>
            {{- end}}

			{{if .PNG -}}
            <input type="radio" id="PNG" name="formatTab" class="tab" {{if not .APNG -}} {{if not .GIF -}} checked {{- end}} {{- end}}>
            <label for="PNG">PNG</label>
            {{- end}}

			{{if .APNG -}}
            <div class="flow" format="APNG">
            	{{range .Package.Stickers -}}
                <div class="sticker" title="{{.Id}}">
                    <img src="./APNG/{{.Id}}.png">
                </div>
                {{- end}}
            </div>
            {{- end}}

			{{if .GIF -}}
            <div class="flow" format="GIF">
            	{{range .Package.Stickers -}}
                <div class="sticker" title="{{.Id}}">
                    <img src="./GIF/{{.Id}}.gif">
                </div>
                {{- end}}
            </div>
            {{- end}}

            {{if .PNG -}}
            <div class="flow" format="PNG">
            	{{range .Package.Stickers -}}
                <div class="sticker" title="{{.Id}}">
                    <img src="./PNG/{{.Id}}.png">
                </div>
                {{- end}}
            </div>
            {{- end}}
        </div>
    </body>
</html>
`
