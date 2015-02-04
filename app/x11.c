// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build linux,!android,!glx

#include "_cgo_export.h"
#include <EGL/egl.h>
#include <GLES2/gl2.h>
#include <X11/Xlib.h>
#include <stdio.h>
#include <stdlib.h>

static Window new_window(Display *x_dpy, EGLDisplay e_dpy, int w, int h, EGLContext *ctx, EGLSurface *surf) {
	static const EGLint attribs[] = {
		EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
		EGL_SURFACE_TYPE, EGL_WINDOW_BIT,
		EGL_BLUE_SIZE, 8,
		EGL_GREEN_SIZE, 8,
		EGL_RED_SIZE, 8,
		EGL_DEPTH_SIZE, 16,
		EGL_CONFIG_CAVEAT, EGL_NONE,
		EGL_NONE
	};
	EGLConfig config;
	EGLint num_configs;
	if (!eglChooseConfig(e_dpy, attribs, &config, 1, &num_configs)) {
		fprintf(stderr, "eglChooseConfig failed\n");
		exit(1);
	}
	EGLint vid;
	if (!eglGetConfigAttrib(e_dpy, config, EGL_NATIVE_VISUAL_ID, &vid)) {
		fprintf(stderr, "eglGetConfigAttrib failed\n");
		exit(1);
	}

	XVisualInfo visTemplate;
	visTemplate.visualid = vid;
	int num_visuals;
	XVisualInfo *visInfo = XGetVisualInfo(x_dpy, VisualIDMask, &visTemplate, &num_visuals);
	if (!visInfo) {
		fprintf(stderr, "XGetVisualInfo failed\n");
		exit(1);
	}

	Window root = RootWindow(x_dpy, DefaultScreen(x_dpy));
	XSetWindowAttributes attr;

	attr.colormap = XCreateColormap(x_dpy, root, visInfo->visual, AllocNone);
	if (!attr.colormap) {
		fprintf(stderr, "Failed to create colormap\n");
		exit(1);
	}

	attr.event_mask = StructureNotifyMask | ExposureMask |
		ButtonPressMask | ButtonReleaseMask | ButtonMotionMask;
	Window win = XCreateWindow(
		x_dpy, root, 0, 0, w, h, 0, visInfo->depth, InputOutput,
		visInfo->visual, CWEventMask | CWColormap, &attr);
	XFree(visInfo);

	XSizeHints sizehints;
	sizehints.width  = w;
	sizehints.height = h;
	sizehints.flags = USSize;
	XSetNormalHints(x_dpy, win, &sizehints);
	XSetStandardProperties(x_dpy, win, "App", "App", None, (char **)NULL, 0, &sizehints);

	static const EGLint ctx_attribs[] = {
		EGL_CONTEXT_CLIENT_VERSION, 2,
		EGL_NONE
	};
	*ctx = eglCreateContext(e_dpy, config, EGL_NO_CONTEXT, ctx_attribs);
	if (!*ctx) {
		fprintf(stderr, "eglCreateContext failed\n");
		exit(1);
	}
	*surf = eglCreateWindowSurface(e_dpy, config, win, NULL);
	if (!*surf) {
		fprintf(stderr, "eglCreateWindowSurface failed\n");
		exit(1);
	}
	return win;
}

// debug event handling
const int D_EVENTS = 0;

static Atom	wm_delete_window;

// Process the specified X event.
static int processEvent(XEvent *ev) {
	switch (ev->type) {
	case ButtonPress:
		if (D_EVENTS) puts("x11.c: XNextEvent:ButtonPress");
		onTouchStart((float)ev->xbutton.x, (float)ev->xbutton.y);
		break;
	case ButtonRelease:
		if (D_EVENTS) puts("x11.c: XNextEvent:ButtonRelease");
		onTouchEnd((float)ev->xbutton.x, (float)ev->xbutton.y);
		break;
	case MotionNotify:
		if (D_EVENTS) puts("x11.c: XNextEvent:MotionNotify");
		onTouchMove((float)ev->xmotion.x, (float)ev->xmotion.y);
		break;
	case Expose:
		if (D_EVENTS) puts("x11.c: XNextEvent:Expose");
		// do nothing
		break;
	case ConfigureNotify:
		if (D_EVENTS) {
			printf("x11.c: XNextEvent:ConfigureNotify w%d h%d\n",
				ev->xconfigure.width, ev->xconfigure.height);
		}
		onResize(ev->xconfigure.width, ev->xconfigure.height);
		break;
	case ClientMessage:
		if (D_EVENTS) puts("x11.c: XNextEvent:ClientMessage");
		if((Atom)ev->xclient.data.l[0] == wm_delete_window) {
			if (D_EVENTS) puts("x11.c WM_DELETE_WINDOW");
			
			return 0;
		}
		break;
	}
	return 1;
}

// Time since beforeT in µs.
static long since(struct timeval *beforeT) {
	struct timeval afterT;
	gettimeofday(&afterT, NULL);
	return ((afterT.tv_sec - beforeT->tv_sec) * 1000000L +
		afterT.tv_usec) - beforeT->tv_usec;
}

const int width = 480;
const int height = 800;

void runApp(void) {
	Display *x_dpy = XOpenDisplay(NULL);
	if (!x_dpy) {
		fprintf(stderr, "XOpenDisplay failed\n");
		exit(1);
	}
	EGLDisplay e_dpy = eglGetDisplay(x_dpy);
	if (!e_dpy) {
		fprintf(stderr, "eglGetDisplay failed\n");
		exit(1);
	}
	EGLint e_major, e_minor;
	if (!eglInitialize(e_dpy, &e_major, &e_minor)) {
		fprintf(stderr, "eglInitialize failed\n");
		exit(1);
	}
	eglBindAPI(EGL_OPENGL_ES_API);
	EGLContext e_ctx;
	EGLSurface e_surf;
	Window win = new_window(x_dpy, e_dpy, width, height, &e_ctx, &e_surf);

	wm_delete_window = XInternAtom(x_dpy, "WM_DELETE_WINDOW", True);
	XSetWMProtocols(x_dpy, win, &wm_delete_window, 1);

	XMapWindow(x_dpy, win);
	if (!eglMakeCurrent(e_dpy, e_surf, e_surf, e_ctx)) {
		fprintf(stderr, "eglMakeCurrent failed\n");
		exit(1);
	}


	// Initialize the geom package.
	onResize(width, height);

	// Setup timers.
	struct timeval lastEventT, lastSwapT, lastFpsUpdateT;
	gettimeofday(&lastEventT, NULL);
	gettimeofday(&lastSwapT, NULL);
	gettimeofday(&lastFpsUpdateT, NULL);
	float fps = 0;

	// Enable vsync. Ignored by the mesa software fallback [LIBGL_ALWAYS_SOFTWARE=0] (0),
	// intel driver (1). OS drivers can be overriden with [vblank_mode=0/1/2]
	eglSwapInterval(x_dpy, 1);
	// Cap the fps if swapBuffers doesn't sync to vblank.
	const int FPS_CAP = 80;
	// Don't congest the X11 event que.
	// 30x/sec cap seems to be responsible enough.
	const int EV_CAP = 30;

	// Main app loop.
	for (;;) {
		if (since(&lastEventT) > 1000000L/30) {
			gettimeofday(&lastEventT, NULL);

			// Count pending events, XNextEvent will block if the X11 buffer is empty.
			int count = XPending(x_dpy);
			while (count--) {
				XEvent ev;
				if (D_EVENTS) printf("x11.c: XNextEvent: %d\n", count);
				XNextEvent(x_dpy, &ev);
				if (!processEvent(&ev)) {
					XDestroyWindow(x_dpy, ev.xclient.window);
					XCloseDisplay(x_dpy);
					exit(0);
				}
			}
		}
		if (since(&lastSwapT) < 1000000L/FPS_CAP) {
			// Spin with step ~0.01 ms.
			usleep(10);
		} else {
			long dT = since(&lastSwapT);
			gettimeofday(&lastSwapT, NULL);
			fps = 1000000.0 / dT;
			onDraw();
			eglSwapBuffers(e_dpy, e_surf);
		}
		if (D_EVENTS) {
			if (since(&lastFpsUpdateT) > 1000000L/1) {
				printf("x11.c: fps: %.2f\n", fps);
				gettimeofday(&lastFpsUpdateT, NULL);
			}
		}
	}
}

