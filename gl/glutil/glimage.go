// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package glutil

import (
	"encoding/binary"
	"image"
	"sync"

	"grate/backend/mobile/f32"
	"grate/backend/mobile/geom"
	"grate/backend/mobile/gl"
)

var glimage struct {
	sync.Once
	quadXY        gl.Buffer
	quadUV        gl.Buffer
	program       gl.Program
	pos           gl.Attrib
	mvp           gl.Uniform
	uvp           gl.Uniform
	inUV          gl.Attrib
	textureSample gl.Uniform
	tintcolor 	  gl.Uniform
}

var R, G, B float32 = 1, 1, 1

func Program() gl.Program {
	return glimage.program
}

func glInit() {
	var err error
	glimage.program, err = CreateProgram(vertexShader, fragmentShader)
	if err != nil {
		panic(err)
	}

	glimage.quadXY = gl.GenBuffer()
	glimage.quadUV = gl.GenBuffer()

	gl.BindBuffer(gl.ARRAY_BUFFER, glimage.quadXY)
	gl.BufferData(gl.ARRAY_BUFFER, gl.STATIC_DRAW, quadXYCoords)
	gl.BindBuffer(gl.ARRAY_BUFFER, glimage.quadUV)
	gl.BufferData(gl.ARRAY_BUFFER, gl.STATIC_DRAW, quadUVCoords)
	
	glimage.tintcolor = gl.GetUniformLocation(glimage.program, "tintColor")

	glimage.pos = gl.GetAttribLocation(glimage.program, "pos")
	glimage.mvp = gl.GetUniformLocation(glimage.program, "mvp")
	glimage.uvp = gl.GetUniformLocation(glimage.program, "uvp")
	glimage.inUV = gl.GetAttribLocation(glimage.program, "inUV")
	glimage.textureSample = gl.GetUniformLocation(glimage.program, "textureSample")
}

// Image bridges between an *image.RGBA and an OpenGL texture.
//
// The contents of the embedded *image.RGBA can be uploaded as a
// texture and drawn as a 2D quad.
//
// The number of active Images must fit in the system's OpenGL texture
// limit. The typical use of an Image is as a texture atlas.
type Image struct {
	*image.RGBA

	Texture   gl.Texture
	texWidth  int
	texHeight int
}

// NewImage creates an Image of the given size.
//
// Both a host-memory *image.RGBA and a GL texture are created.
func NewImage(w, h int) *Image {
	dx := roundToPower2(w)
	dy := roundToPower2(h)

	// TODO(crawshaw): Using VertexAttribPointer we can pass texture
	// data with a stride, which would let us use the exact number of
	// pixels on the host instead of the rounded up power 2 size.
	m := image.NewRGBA(image.Rect(0, 0, dx, dy))

	glimage.Do(glInit)

	img := &Image{
		RGBA:      m.SubImage(image.Rect(0, 0, w, h)).(*image.RGBA),
		Texture:   gl.GenTexture(),
		texWidth:  dx,
		texHeight: dy,
	}
	// TODO(crawshaw): We don't have the context on a finalizer. Find a way.
	// runtime.SetFinalizer(img, func(img *Image) { gl.DeleteTexture(img.Texture) })
	gl.BindTexture(gl.TEXTURE_2D, img.Texture)
	gl.TexImage2D(gl.TEXTURE_2D, 0, dx, dy, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	return img
}

func roundToPower2(x int) int {
	x2 := 1
	for x2 < x {
		x2 *= 2
	}
	return x2
}

// Upload copies the host image data to the GL device.
func (img *Image) Upload() {
	gl.BindTexture(gl.TEXTURE_2D, img.Texture)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, img.texWidth, img.texHeight, gl.RGBA, gl.UNSIGNED_BYTE, img.Pix)
}

// Draw draws the srcBounds part of the image onto a parallelogram, defined by
// three of its corners, in the current GL framebuffer.
func (img *Image) Draw(topLeft, topRight, bottomLeft geom.Point, srcBounds image.Rectangle) {
	// TODO(crawshaw): Adjust viewport for the top bar on android?
	gl.UseProgram(glimage.program)

	{
		// We are drawing a parallelogram PQRS, defined by three of its
		// corners, onto the entire GL framebuffer ABCD. The two quads may
		// actually be equal, but in the general case, PQRS can be smaller,
		// and PQRS is not necessarily axis-aligned.
		//
		//	A +---------------+ B
		//	  |  P +-----+ Q  |
		//	  |    |     |    |
		//	  |  S +-----+ R  |
		//	D +---------------+ C
		//
		// There are two co-ordinate spaces: geom space and framebuffer space.
		// In geom space, the ABCD rectangle is:
		//
		//	(0, 0)           (geom.Width, 0)
		//	(0, geom.Height) (geom.Width, geom.Height)
		//
		// and the PQRS quad is:
		//
		//	(topLeft.X,    topLeft.Y)    (topRight.X, topRight.Y)
		//	(bottomLeft.X, bottomLeft.Y) (implicit,   implicit)
		//
		// In framebuffer space, the ABCD rectangle is:
		//
		//	(-1, +1) (+1, +1)
		//	(-1, -1) (+1, -1)
		//
		// First of all, convert from geom space to framebuffer space. For
		// later convenience, we divide everything by 2 here: px2 is half of
		// the P.X co-ordinate (in framebuffer space).
		px2 := -0.5 + float32(topLeft.X)/geom.Width.Px()
		py2 := +0.5 - float32(topLeft.Y)/geom.Height.Px()
		qx2 := -0.5 + float32(topRight.X)/geom.Width.Px()
		qy2 := +0.5 - float32(topRight.Y)/geom.Height.Px()
		sx2 := -0.5 + float32(bottomLeft.X)/geom.Width.Px()
		sy2 := +0.5 - float32(bottomLeft.Y)/geom.Height.Px()
		// Next, solve for the affine transformation matrix
		//	    [ a00 a01 a02 ]
		//	a = [ a10 a11 a12 ]
		//	    [   0   0   1 ]
		// that maps A to P:
		//	a × [ -1 +1 1 ]' = [ 2*px2 2*py2 1 ]'
		// and likewise maps B to Q and D to S. Solving those three constraints
		// implies that C maps to R, since affine transformations keep parallel
		// lines parallel. This gives 6 equations in 6 unknowns:
		//	-a00 + a01 + a02 = 2*px2
		//	-a10 + a11 + a12 = 2*py2
		//	+a00 + a01 + a02 = 2*qx2
		//	+a10 + a11 + a12 = 2*qy2
		//	-a00 - a01 + a02 = 2*sx2
		//	-a10 - a11 + a12 = 2*sy2
		// which gives:
		//	a00 = (2*qx2 - 2*px2) / 2 = qx2 - px2
		// and similarly for the other elements of a.
		glimage.mvp.WriteAffine(&f32.Affine{{
			qx2 - px2,
			px2 - sx2,
			qx2 + sx2,
		}, {
			qy2 - py2,
			py2 - sy2,
			qy2 + sy2,
		}})
	}

	{
		// Mapping texture co-ordinates is similar, except that in texture
		// space, the ABCD rectangle is:
		//
		//	(0,0) (1,0)
		//	(0,1) (1,1)
		//
		// and the PQRS quad is always axis-aligned. First of all, convert
		// from pixel space to texture space.
		w := float32(img.texWidth)
		h := float32(img.texHeight)
		px := float32(srcBounds.Min.X-img.Rect.Min.X) / w
		py := float32(srcBounds.Min.Y-img.Rect.Min.Y) / h
		qx := float32(srcBounds.Max.X-img.Rect.Min.X) / w
		sy := float32(srcBounds.Max.Y-img.Rect.Min.Y) / h
		// Due to axis alignment, qy = py and sx = px.
		//
		// The simultaneous equations are:
		//	  0 +   0 + a02 = px
		//	  0 +   0 + a12 = py
		//	a00 +   0 + a02 = qx
		//	a10 +   0 + a12 = qy = py
		//	  0 + a01 + a02 = sx = px
		//	  0 + a11 + a12 = sy
		glimage.uvp.WriteAffine(&f32.Affine{{
			qx - px,
			0,
			px,
		}, {
			0,
			sy - py,
			py,
		}})
	}

	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, img.Texture)
	gl.Uniform1i(glimage.textureSample, 0)

	gl.BindBuffer(gl.ARRAY_BUFFER, glimage.quadXY)
	gl.EnableVertexAttribArray(glimage.pos)
	gl.VertexAttribPointer(glimage.pos, 2, gl.FLOAT, false, 0, 0)

	gl.BindBuffer(gl.ARRAY_BUFFER, glimage.quadUV)
	gl.EnableVertexAttribArray(glimage.inUV)
	gl.VertexAttribPointer(glimage.inUV, 2, gl.FLOAT, false, 0, 0)
	
	gl.Uniform4f(glimage.tintcolor, R, G, B, 1)

	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)

	gl.DisableVertexAttribArray(glimage.pos)
	gl.DisableVertexAttribArray(glimage.inUV)
}

var quadXYCoords = f32.Bytes(binary.LittleEndian,
	-1, +1, // top left
	+1, +1, // top right
	-1, -1, // bottom left
	+1, -1, // bottom right
)

var quadUVCoords = f32.Bytes(binary.LittleEndian,
	0, 0, // top left
	1, 0, // top right
	0, 1, // bottom left
	1, 1, // bottom right
)

const vertexShader = `#version 100
uniform mat3 mvp;
uniform mat3 uvp;
attribute vec3 pos;
attribute vec2 inUV;
varying vec2 UV;
void main() {
	vec3 p = pos;
	p.z = 1.0;
	gl_Position = vec4(mvp * p, 1);
	UV = (uvp * vec3(inUV, 1)).xy;
}
`

const fragmentShader = `#version 100
precision mediump float;
varying vec2 UV;
uniform sampler2D textureSample;

uniform vec4 tintColor;

void main(){
	vec4 tex = texture2D(textureSample, UV);
	if (tintColor.r != 1.0 || tintColor.g != 1.0 || tintColor.b != 1.0) {
		gl_FragColor = tex*vec4(tintColor.r, tintColor.g, tintColor.b, tex.a);
	} else {
		gl_FragColor = tex;
	}
}
`
