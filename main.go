package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/go-gl/gl/v3.2-core/gl"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/golang-ui/nuklear/nk"
	"github.com/xlab/closer"
)

const (
	winWidth  = 400
	winHeight = 150

	maxVertexBuffer  = 512 * 1024
	maxElementBuffer = 128 * 1024

	defaultAddress = "10.41.86.12:5800"
	pingFormat     = "http://%s/ping"
	cameraFormat   = "http://%s/camera"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	if err := glfw.Init(); err != nil {
		closer.Fatalln(err)
	}
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 2)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	win, err := glfw.CreateWindow(winWidth, winHeight, "Jetson Commander", nil, nil)
	if err != nil {
		closer.Fatalln(err)
	}
	win.MakeContextCurrent()

	width, height := win.GetSize()
	log.Printf("glfw: created window %dx%d", width, height)

	if err := gl.Init(); err != nil {
		closer.Fatalln("opengl: init failed:", err)
	}
	gl.Viewport(0, 0, int32(width), int32(height))

	ctx := nk.NkPlatformInit(win, nk.PlatformInstallCallbacks)

	atlas := nk.NewFontAtlas()
	nk.NkFontStashBegin(&atlas)
	sansFont := nk.NkFontAtlasAddFromBytes(atlas, MustAsset("assets/FreeSans.ttf"), 16, nil)
	// sansFont := nk.NkFontAtlasAddDefault(atlas, 16, nil)
	nk.NkFontStashEnd()
	if sansFont != nil {
		nk.NkStyleSetFont(ctx, sansFont.Handle())
	}

	exitC := make(chan struct{}, 1)
	doneC := make(chan struct{}, 1)
	pingC := make(chan bool, 1)

	closer.Bind(func() {
		close(exitC)
		<-doneC
	})

	state := &State{
		addressBuffer: make([]byte, 20),
		layout:        [4]int32{0, 1, 2, 3},
	}

	state.log("Start...")

	state.addressLenght = int32(len(defaultAddress))
	copy(state.addressBuffer, []byte(defaultAddress))
	state.pingAddress = fmt.Sprintf(pingFormat, defaultAddress)
	state.cameraAddress = fmt.Sprintf(cameraFormat, defaultAddress)

	go ping_task(pingC, state)

	fpsTicker := time.NewTicker(time.Second / 30)
	for {
		select {
		case <-exitC:
			state.endProcess()
			nk.NkPlatformShutdown()
			glfw.Terminate()
			fpsTicker.Stop()
			close(doneC)
			return
		case <-fpsTicker.C:
			if win.ShouldClose() {
				close(exitC)
				continue
			}
			glfw.PollEvents()
			gfxMain(win, ctx, pingC, state)
		case online := <-pingC:
			state.online = online
			if online {
				state.logf("[INFO] %s online", state.pingAddress)
			} else {
				state.logf("[INFO] %s offline", state.pingAddress)
			}
			continue
		}
	}
}

func ping_task(pingResult chan bool, state *State) {
	pingTicker := time.NewTicker(time.Second)
	for {
		<-pingTicker.C
		online := ping(state)
		pingResult <- online
		if online {
			pingTicker.Stop()
		}
	}
}

func gfxMain(win *glfw.Window, ctx *nk.Context, pingC chan bool, state *State) {
	nk.NkPlatformNewFrame()

	// Layout
	width, height := win.GetSize()
	bounds := nk.NkRect(0, 0, float32(width), float32(height))
	update := nk.NkBegin(ctx, "Demo", bounds, nk.WindowBorder)

	if update > 0 {
		nk.NkLayoutRowDynamic(ctx, 30, 2)
		{
			nk.NkEditString(ctx, nk.EditField, state.addressBuffer, &state.addressLenght, 20, nk.NkFilterDefault)
			state.pingAddress = fmt.Sprintf(pingFormat, state.addressBuffer[:state.addressLenght])
			state.cameraAddress = fmt.Sprintf(cameraFormat, state.addressBuffer[:state.addressLenght])
			if nk.NkButtonLabel(ctx, "Ping") > 0 {
				go ping_task(pingC, state)
			}
		}

		if state.online {
			nk.NkLayoutRowDynamic(ctx, 30, 5)
			{
				if nk.NkButtonLabel(ctx, "0") > 0 {
					launchGst(state, "default", "15")
				}
				if nk.NkButtonLabel(ctx, "1") > 0 {
					launchGst(state, "only-1", "30")
				}

				if nk.NkButtonLabel(ctx, "2") > 0 {
					launchGst(state, "only-2", "30")
				}
				if nk.NkButtonLabel(ctx, "3") > 0 {
					launchGst(state, "only-3", "30")
				}

				if nk.NkButtonLabel(ctx, "4") > 0 {
					launchGst(state, "only-4", "30")
				}
			}
			/*
				{
					if nk.NkButtonLabel(ctx, "M") > 0 {
						launchGst(state, "default", "15")
					}

					cameraId := []string{"1", "2", "3", "4"}
					size := nk.NkVec2(60, 200)
					state.layout[0] = nk.NkCombo(ctx, cameraId, 4, state.layout[0], 25, size)
					state.layout[1] = nk.NkCombo(ctx, cameraId, 4, state.layout[1], 25, size)
					state.layout[2] = nk.NkCombo(ctx, cameraId, 4, state.layout[2], 25, size)
					state.layout[3] = nk.NkCombo(ctx, cameraId, 4, state.layout[3], 25, size)
					state.log(state.layout)

				}*/
		}

		nk.NkLayoutRowDynamic(ctx, 15, 1)
		{
			nk.NkLabel(ctx, state.logLine(0), nk.TextLeft)
			nk.NkLabel(ctx, state.logLine(1), nk.TextLeft)
			nk.NkLabel(ctx, state.logLine(2), nk.TextLeft)
		}
	}
	nk.NkEnd(ctx)

	// Render
	gl.Viewport(0, 0, int32(width), int32(height))
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.ClearColor(0, 0, 0, 0)
	nk.NkPlatformRender(nk.AntiAliasingOn, maxVertexBuffer, maxElementBuffer)
	win.SwapBuffers()
}

type State struct {
	online        bool
	streamWindow  *exec.Cmd
	addressLenght int32
	addressBuffer []byte

	pingAddress   string
	cameraAddress string

	layout [4]int32

	logBuffer [3]string
	logCursor int
}

func (self *State) endProcess() {
	if self.streamWindow != nil {
		self.streamWindow.Process.Kill()
	}
}

func (self *State) logf(format string, args ...interface{}) {
	self.log(fmt.Sprintf(format, args))
}

func (self *State) logLine(i int) string {
	return self.logBuffer[((self.logCursor + len(self.logBuffer) + i) % len(self.logBuffer))]
}

func (self *State) log(msg interface{}) {
	self.logBuffer[self.logCursor] = fmt.Sprintf("%s %s", time.Now().Format(time.Kitchen), msg)
	self.logCursor = (self.logCursor + 1) % len(self.logBuffer)
	log.Println(msg)
}

func ping(state *State) bool {
	rs, err := http.Get(state.pingAddress)
	// Process response
	if err != nil {
		state.log(err) // More idiomatic way would be to print the error and die unless it's a serious error
		return false
	} else {
		defer rs.Body.Close()

		_, err := ioutil.ReadAll(rs.Body)
		if err != nil {
			state.log(err)
			return false
		} else {
			return true
		}
	}
}

func launchGst(state *State, layout, hertz string) {
	//	if state.streamWindow != nil {
	//		err := state.streamWindow.Process.Kill()
	//		if err != nil {
	//			log.Fatal(err) // More idiomatic way would be to print the error and die unless it's a serious error
	//		}
	//		state.streamWindow.Wait()
	//		state.streamWindow = nil
	//	}

	rs, err := http.Get(fmt.Sprintf("%s?layout=%s&hertz=%s", state.cameraAddress, layout, hertz))
	// Process response
	if err != nil {
		state.log(err)
		state.online = false
		return
	}
	defer rs.Body.Close()

	bodyBytes, err := ioutil.ReadAll(rs.Body)
	if err != nil {
		state.log(err)
		return
	}

	bodyString := string(bodyBytes)
	state.logf("[INFO] %s", strings.Fields(bodyString))

	if state.streamWindow == nil {
		cmd := exec.Command("gst-launch-1.0", strings.Fields(bodyString)...)
		var out bytes.Buffer
		cmd.Stderr = &out
		err = cmd.Start()
		if err != nil {
			state.logf("in all caps: %q", out.String())
		} else {
			state.streamWindow = cmd
		}
	}
}
