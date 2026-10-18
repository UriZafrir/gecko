// xterm's WebGL renderer, fixed for see-through terminals.
//
// Gecko's terminals have a transparent background wherever something shows
// through them: the desktop app's window opacity and the picture themes
// (allowTransparency is always on). Two bugs in @xterm/addon-webgl 0.19
// then hit light themes hard, on desktops only (phones use the DOM
// renderer):
//
// 1. Glyphs are blended with blendFunc(SRC_ALPHA, ONE_MINUS_SRC_ALPHA),
//    which multiplies the canvas's alpha by itself: an anti-aliased edge
//    with 50% coverage comes out 25% opaque. The page composites the
//    canvas as premultiplied, so dark text on a light background loses
//    most of its edges and looks thin and washed out, selections nearly
//    vanish, and bold reads as regular. Light text on a dark background
//    hardly shows it (what it loses is dark there anyway). Blending the
//    alpha channel as plain "over" (ONE, ONE_MINUS_SRC_ALPHA) fixes it.
// 2. A dim cell on the default background gets a rectangle in the theme's
//    background color, always opaque: with a transparent background that
//    is a black box behind every bit of dim text. The rectangle now takes
//    the background's own alpha, so it is invisible when that is.

let blendFixed = false;

// Patches WebGL2 blending once for the page; nothing else here uses WebGL.
export function fixWebglBlending() {
  if (blendFixed || typeof WebGL2RenderingContext === 'undefined') return;
  blendFixed = true;
  const proto = WebGL2RenderingContext.prototype;
  const blendFunc = proto.blendFunc;
  proto.blendFunc = function (src, dst) {
    if (src === this.SRC_ALPHA && dst === this.ONE_MINUS_SRC_ALPHA) return this.blendFuncSeparate(src, dst, this.ONE, dst);
    return blendFunc.call(this, src, dst);
  };
}

// Attribute bits of xterm's packed cell colors.
const FG_INVERSE = 0x4000000;
const CM_MASK = 0x3000000; // 0: default color
const RECT_ALPHA = 7; // rectangle vertex layout: x, y, w, h, r, g, b, a

// Call after loading the WebGL addon. It reaches into the addon's renderer
// (private fields), so it checks every step and does nothing if the
// addon's insides ever change shape.
export function fixWebglDim(addon) {
  const rr = addon?._renderer?._rectangleRenderer?.value;
  const proto = rr && Object.getPrototypeOf(rr);
  if (!proto || proto.geckoDimFix || typeof proto._updateRectangle !== 'function') return;
  const update = proto._updateRectangle;
  proto._updateRectangle = function (vertices, offset, fg, bg, ...rest) {
    update.call(this, vertices, offset, fg, bg, ...rest);
    const a = this._themeService?.colors?.background?.rgba;
    if (!(fg & FG_INVERSE) && !(bg & CM_MASK) && typeof a === 'number' && vertices?.attributes) {
      vertices.attributes[offset + RECT_ALPHA] = (a & 0xff) / 255;
    }
  };
  proto.geckoDimFix = true;
}
