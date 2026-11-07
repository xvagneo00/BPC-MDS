package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andreykaipov/goobs"
	goobs_inputs "github.com/andreykaipov/goobs/api/requests/inputs"
	goobs_sources "github.com/andreykaipov/goobs/api/requests/sources"
	goobs_ui "github.com/andreykaipov/goobs/api/requests/ui"
)

var data struct {
	Body string
	Img  string
}

var client *goobs.Client

const studioonf = `	<div class="container text-center">
		<div class="row align-items-center">
			<div class="col-5">
				<img src="%s" class="img-fluid border">
			</div>
			<div class="col-2">
				<button type="submit" form="form" name="transit" value="transit" class="btn">Transit</button>
				<button type="submit" form="form" name="studiomode" value="disable" class="btn">Disable Studio Mode</button>
			</div>
			<div class="col-5">
				<img src="%s" class="img-fluid border">
			</div>
		</div>
	</div>`

const studiooff = `	<div class="container text-center">
		<div class="row align-items-center">
			<div class="col">
				<button type="submit" form="form" name="studiomode" value="enable" class="btn">Enable Studio Mode</button>
			</div>
		</div>
	</div>`

const itemrowf = `					<tr><td>%s</td><td>%v</td></tr>
`
const itemf = `		<div class="container text-center border m-6 p-2 w-25">
			<img src="%s" class="img-fluid border">
			<div><h3>%s</h3></div>
			<table class="table">
				<thead>
					<tr>
						<th scope="col">name</th>
						<th scope="col">value</th>
					</tr>
				</thead>
				<tbody>
%s
				</tbody>
			</table>
		</div>
`

const pagef = `<!DOCTYPE html>
<html>
<head>
	<meta http-equiv="refresh" content="3">
	<link href="https://cdn.jsdelivr.net/npm/bootstrap@6.0.0-alpha.1/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-B/GM4XqrwHnWXNOWMbloTmrYXZg10cakYGmpfsR/bbzQ6JAJI4ihuyADKLnBgrCe" crossorigin="anonymous">
	<title>Director</title>
</head>
<body class="m-5">
%s
	<div class="d-flex flex-wrap justify-content-center">
%s
	</div>
	<form id="form" method="post" action=""></form>
</body>
</html>`

var page atomic.Value
var mu sync.Mutex

func makescreenshot(uuid string) (string, error) {
	params := goobs_sources.NewGetSourceScreenshotParams().WithSourceUuid(uuid).WithImageCompressionQuality(-1).WithImageFormat("png")
	mu.Lock()
	screenshot, err := client.Sources.GetSourceScreenshot(params)
	mu.Unlock()
	if err != nil {
		return "", err
	}
	return screenshot.ImageData, nil
}

func render() {
	for {
		studio := studiooff

		mu.Lock()
		studiomode, err := client.Ui.GetStudioModeEnabled()
		mu.Unlock()
		if err != nil {
			log.Fatal(err)
		}

		if studiomode.StudioModeEnabled {
			var studioimgs [2]string

			mu.Lock()
			preview, err := client.Scenes.GetCurrentPreviewScene()
			mu.Unlock()
			if err != nil {
				log.Fatal(err)
			}
			mu.Lock()
			program, err := client.Scenes.GetCurrentProgramScene()
			mu.Unlock()
			if err != nil {
				log.Fatal(err)
			}
			studioimgs[0] = preview.SceneUuid
			studioimgs[1] = program.SceneUuid

			for i := range studioimgs {
				studioimgs[i], err = makescreenshot(studioimgs[i])
				if err != nil {
					log.Fatal(err)
				}
			}

			studio = fmt.Sprintf(studioonf, studioimgs[0], studioimgs[1])
		}

		mu.Lock()
		list, err := client.Inputs.GetInputList()
		mu.Unlock()
		if err != nil {
			log.Fatal(err)
		}
		var items strings.Builder
		for _, v := range list.Inputs {
			params := goobs_inputs.NewGetInputSettingsParams().WithInputUuid(v.InputUuid)
			mu.Lock()
			settings, err := client.Inputs.GetInputSettings(params)
			mu.Unlock()
			if err != nil {
				log.Fatal(err)
			}
			var rows strings.Builder
			for l, w := range settings.InputSettings {
				rows.WriteString(fmt.Sprintf(itemrowf, l, w))
			}
			screenshot, _ := makescreenshot(v.InputUuid)
			items.WriteString(fmt.Sprintf(itemf, screenshot, v.InputName, rows.String()))
		}

		page.Store(fmt.Sprintf(pagef, studio, items.String()))
		time.Sleep(5 * time.Second)
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, _ := range r.PostForm {
			switch k {
			case "transit":
				if r.PostForm.Get(k) == "transit" {
					mu.Lock()
					_, err := client.Transitions.TriggerStudioModeTransition()
					mu.Unlock()
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				}
			case "studiomode":
				params := goobs_ui.NewSetStudioModeEnabledParams().WithStudioModeEnabled(r.PostForm.Get(k) == "enable")
				mu.Lock()
				_, err := client.Ui.SetStudioModeEnabled(params)
				mu.Unlock()
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			log.Print(k)
		}
	}

	_, err := fmt.Fprint(w, page.Load().(string))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func main() {
	var err error
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "%s [password]\n", os.Args[0])
		os.Exit(1)
	}

	log.Printf("password: %s", os.Args[1])

	client, err = goobs.New("localhost:4455", goobs.WithPassword(os.Args[1]))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect()

	version, err := client.General.GetVersion()
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("OBS Studio version: %s\n", version.ObsVersion)
	log.Printf("Server protocol version: %s\n", version.ObsWebSocketVersion)
	log.Printf("Client protocol version: %s\n", goobs.ProtocolVersion)
	log.Printf("Client library version: %s\n", goobs.LibraryVersion)

	list, _ := client.Inputs.GetInputList()
	for _, v := range list.Inputs {
		fmt.Println(v.InputName)
	}

	params := goobs_sources.NewGetSourceScreenshotParams().WithSourceName("Scene").WithImageCompressionQuality(-1).WithImageFormat("png")
	screenshot, err := client.Sources.GetSourceScreenshot(params)
	if err != nil {
		log.Fatal(err)
	}
	data.Img = screenshot.ImageData

	page.Store("")
	go render()

	http.HandleFunc("/director", handler)
	log.Fatal(http.ListenAndServe("localhost:31337", nil))
}
