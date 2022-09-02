package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"net/url"
	"os"
	"runtime"
	"time"
	"unsafe"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/gorilla/websocket"
)

const (
	W         = 500
	H         = 500
	fps       = time.Second / 2
	pi        = 3.1415926535897932384626433832795028841971693993751058209749445923078164062862089986280348253421170679
	viewRange = 1000
	// The first point of the array of the Vectors in the ray struct
	// is used as initial point for subsequent rays, for eg.
	// Consider for following array : [{0, 1}, {13, 23}, {23, 24}, {1, 23}]
	// There will be a total of 3 rays constructed from the array and
	// they will be intersecting [{0, 1}, {13, 23}], [{0, 1}, {23, 24}]
	// and [{0, 1}, {1, 23}] respectively
	RAY_TYPE_CENTERED = 0x0
	// The initial point in the array of vectors in the ray struct
	// is every other vector, or the index has the index 2n where
	// n is the number of ray being considered, for eg.

	// Consider for following array : [{0, 1}, {13, 23}, {23, 24}, {1, 23}]
	// There will be a total of 2 rays constructed from the array and
	// they will be intersecting [{0, 1}, {13, 23}] and [{23, 24}, {1, 23}]
	// respectively
	RAY_TYPE_STRIP = 0x1
	pointByteSize  = int32(13 * 4)
)

var (
	viewMat                         mgl32.Mat4
	projMat                         mgl32.Mat4
	defaultViewMat                  mgl32.Mat4
	AddState                        byte
	program                         uint32
	MouseX                          float64
	MouseY                          float64
	CurrPoint                       mgl32.Vec2
	Btns                            []*Button
	BtnState                        = byte('C')
	eyePos                          mgl32.Vec3
	LookAt                          mgl32.Vec3
	MouseRay                        *Ray
	framesDrawn                     int
	Ident                           = mgl32.Ident4()
	endianness                      binary.ByteOrder
	coordBytes                      []byte
	bytesToU64                      func(inputBytes []byte) uint64
	lenPoints                       uint16
	maxWorldX, maxWorldY, maxWorldZ float32
	addr                            = flag.String("addr", "localhost:6969", "Snek Server address with port")
	c                               *websocket.Conn
	score                           uint32
	gameEnded                       bool
	lenInt                          uint8
	to_be_rendered                  []*Point
)

func main() {
	flag.Parse()
	u := url.URL{Scheme: "ws", Host: *addr}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	orDie(err)
	defer c.Close()

	buf := [2]byte{}
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0xABCD)

	switch buf {
	case [2]byte{0xCD, 0xAB}:
		endianness = binary.LittleEndian
	case [2]byte{0xAB, 0xCD}:
		endianness = binary.BigEndian
	default:
		panic("Could not determine native endianness.")
	}
	runtime.LockOSThread()
	orDie(glfw.Init())
	// Close glfw when main exits
	defer glfw.Terminate()
	// Window Properties

	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	// Create the window with the above hints
	window, err := glfw.CreateWindow(W, H, "Snek3D-Frontend", nil, nil)
	orDie(err)
	window.Focus()
	window.Maximize()
	window.Restore()
	// Load the icon file
	icoFile, err := os.Open("ico.png")
	orDie(err)
	// decode the file to an image.Image
	ico, err := png.Decode(icoFile)
	orDie(err)
	window.SetIcon([]image.Image{ico})
	window.MakeContextCurrent()
	window.SetKeyCallback(HandleKeys)
	// OpenGL Initialization
	// Check for the version
	//version := gl.GoStr(gl.GetString(gl.VERSION))
	//	fmt.Println("OpenGL Version", version)
	// Read the vertex and fragment shader files
	vertexShader, err := ioutil.ReadFile("vertex.vert")
	orDie(err)
	vertexShader = append(vertexShader, []byte("\x00")...)
	fragmentShader, err := ioutil.ReadFile("frag.frag")
	orDie(err)
	fragmentShader = append(fragmentShader, []byte("\x00")...)

	orDie(gl.Init())

	// Set the function for handling errors
	gl.DebugMessageCallback(func(source, gltype, id, severity uint32, length int32, message string, userParam unsafe.Pointer) {
		panic(fmt.Sprintf("%d, %d, %d, %d, %d, %s \n", source, gltype, severity, id, length, message))

	}, nil)
	// Create an OpenGL "Program" and link it for current drawing
	prog, err := newProg(string(vertexShader), string(fragmentShader))
	orDie(err)
	// Check for the version
	// Main draw loop

	// Set the refresh function for the window
	// Use this program
	gl.UseProgram(prog)
	// Calculate the projection matrix
	projMat = mgl32.Ident4()
	// set the value of Projection matrix
	UpdateUniformMat4fv("projection", program, &projMat[0])
	// Set the value of view matrix
	UpdateView(
		mgl32.Vec3{0, 0, -1},
		mgl32.Vec3{0, 0, 1},
	)
	program = prog
	// GLFW Initialization
	CurrPoint = mgl32.Vec2{0, 0}
	eyePos = mgl32.Vec3{0, 0, 1}
	WhiteCube := NewShape(Ident, program, []*Point{
		PC(1, 1, 1, 1, 1, 1, 1),
		PC(-1, 1, 1, 1, 1, 1, 1),
		PC(-1, -1, 1, 1, 1, 1, 1),
		PC(-1, -1, -1, 1, 1, 1, 1),
		PC(1, -1, -1, 1, 1, 1, 1),
		PC(1, 1, -1, 1, 1, 1, 1),
		PC(1, -1, 1, 1, 1, 1, 1),
		PC(-1, 1, -1, 1, 1, 1, 1),
	}...)
	RedCube := NewShape(Ident, program, []*Point{
		PC(1, 1, 1, 1, 0, 0, 1),
		PC(-1, 1, 1, 1, 0, 0, 1),
		PC(-1, -1, 1, 1, 0, 0, 1),
		PC(-1, -1, -1, 1, 0, 0, 1),
		PC(1, -1, -1, 1, 0, 0, 1),
		PC(1, 1, -1, 1, 0, 0, 1),
		PC(1, -1, 1, 1, 0, 0, 1),
		PC(-1, 1, -1, 1, 0, 0, 1),
	}...)
	WhiteCube.SetTypes(gl.LINE_LOOP)
	RedCube.SetTypes(gl.LINE_LOOP)
	WhiteCube.GenVao()
	RedCube.GenVao()
	_, metaRaw, err := c.ReadMessage()
	fmt.Println(metaRaw)
	orDie(err)
	lenInt = metaRaw[0] / 8
	fmt.Println("lenInt")
	fmt.Println(lenInt)
	switch lenInt {
	case 1:
		bytesToU64 = func(a []byte) uint64 { return uint64(a[0]) }
	case 2:
		bytesToU64 = func(a []byte) uint64 { return uint64(endianness.Uint16(a)) }
	case 4:
		bytesToU64 = func(a []byte) uint64 { return uint64(endianness.Uint32(a)) }
	case 8:
		bytesToU64 = endianness.Uint64
	}
	maxWorldX, maxWorldY, maxWorldZ = parseCoords(metaRaw[1 : 1+3*lenInt])
	worldR, worldG, worldB := parseColors(metaRaw[1+3*lenInt : 4+3*lenInt])
	gl.ClearColor(worldR, worldG, worldB, 1)
	for !window.ShouldClose() {
		time.Sleep(fps)
		// Clear everything that was drawn previously
		gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
		// Actually draw something
		//		b.Draw()
		framesDrawn++
		fmt.Fprintf(os.Stderr, "Snake kitna lamba hai: %d", len(to_be_rendered)-1)
		for _, v := range to_be_rendered {
			UpdateUniformVec4("current_color", program, &v.C[0])
			WhiteCube.ModelMat = mgl32.Translate3D(v.X(), v.Y(), v.Z())
			WhiteCube.Draw()
			os.Stderr.WriteString("Here?")
		}
		// fnt.GlyphMap['e'].Draw()
		// display everything that was drawn
		window.SwapBuffers()
		// check for any events
		glfw.PollEvents()
	}
}

func NextFrame() []*Point {
	_, dataRaw, err := c.ReadMessage()
	orDie(err)
	numPoints := binary.BigEndian.Uint32(dataRaw[0:4])
	if numPoints == 0 {
		fmt.Println("Game Over")
		_, scoreRaw, err := c.ReadMessage()
		orDie(err)
		score = binary.BigEndian.Uint32(scoreRaw)
		gameEnded = true
	}
	points := make([]*Point, numPoints)
	for i := 0; i < int(numPoints); i++ {
		pointRaw := dataRaw[4+i*3*int(lenInt) : 4+i*int(3*lenInt+3)]
		points[i] = parsePoint(pointRaw)
	}
	return points
}

func parseCoords(size []byte) (float32, float32, float32) {
	x := bytesToU64(size[0:lenInt])
	y := bytesToU64(size[lenInt : 2*lenInt])
	z := bytesToU64(size[2*lenInt : 3*lenInt])
	return float32(x), float32(y), float32(z)
}

func parseColors(colors []byte) (float32, float32, float32) {
	r := float32(colors[0]) / 255
	g := float32(colors[1]) / 255
	b := float32(colors[2]) / 255
	return r, g, b
}

func parsePoint(point []byte) *Point {
	x, y, z := parseCoords(point[0 : 3*lenInt])
	r, g, b := parseColors(point[3*lenInt : 3*lenInt+3])
	return PC(x, y, z, r, g, b, 1)
}
