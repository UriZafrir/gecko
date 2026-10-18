# Phone and tablet

[Docs](README.md)

<img src="img/phone.png" width="320" align="right" alt="Gecko on a phone, approving a Codex command from the agent dock">

By default the daemon listens on `127.0.0.1:7681` only. To use it from your
phone:

- **Tailscale (recommended):** `tailscale serve 7681`. You get HTTPS, so
  clipboard and notifications work fully.
- **LAN:** `gecko daemon --listen 0.0.0.0:7681`, or set `"listen"` in the
  config.

Then pick **Open on another device** in the palette and scan the QR code. The
link carries your access token. It is kept as a cookie, so treat the link like
a password.

## As an app on the home screen (PWA)

The page installs as a web app, with no store and nothing to download:

- **iPhone and iPad:** open the link in Safari, tap **Share**, then **Add to
  Home Screen**.
- **Android:** open the link in Chrome, open the menu, then **Install app**
  (or **Add to Home screen**).

It then opens full screen like an app, under the status bar and clear of the
notch and the home indicator. The **Settings** button at the right of the tab
bar has what the native apps keep in their own menus: the theme, the text
size, the key bar and the compose box, plus search, selecting text and
renaming the tab. Pinching a terminal changes its text size; the page itself
never zooms. With the keyboard up, the key bar sits right above it. When
the machine can't be reached, the app still opens and connects by itself
once it can, and after an upgrade it reloads into the new version.

On Android there is also a native app with notifications that reach you
when it's closed: see [Android app](android.md).

Touch works like a native terminal app: drag to scroll by the pixel, flick
to coast like a web page, and pull past either end for a stretch that springs
back (tmux, vim and less scroll too when they use the mouse); drag the thumb
on the right to jump through the scrollback; pinch to zoom the text, swipe with two
fingers or on the tab bar to change tabs, and swipe in from the left edge for
the drawer.

The mobile (compact) layout is for windows under 720 CSS px wide or under
501 tall: phones in portrait and landscape, iPad Split View and Slide Over,
split screens, folded foldables. Tablets (an iPad mini in portrait too) and
unfolded foldables get the desktop layout with the sidebar. It has:

- A drawer with workspaces, agents and machines.
- A compact tab bar: just the tabs' titles (the workspace, machine, repo and
  folder are in the drawer, and in a tab's long-press menu).
- The agents as one slim row of chips above the keys: a chip shows the
  agent, its tab and how long; tap it for its last line and the answers (or
  quick replies), tap again to close. One that needs you is tinted. The
  button at the start folds the row to a count ("2 agents · 1 needs you").
- A key bar: esc, tab, sticky ctrl/alt, arrows, `^C`, pgup/pgdn, select,
  paste.
- Wider and taller windows (tablets, iPad mini included) keep the sidebar
  beside the terminal, as on a computer.
- A **compose** box for writing prompts with your keyboard's autocorrect.
  Multi-line text goes to agents as a single bracketed paste.
- Voice typing straight into the terminal (Android): what the keyboard
  writes reaches the shell or the agent as you speak, and its corrections
  (a word replaced, a comma for a space, a word taken back) as backspaces
  and the new text, so the line ends up as what you said. Enter or a key
  from the key bar while it's still listening sends the phrase first. In
  the compose box, Enter mid-phrase sends the whole phrase once it's in.

