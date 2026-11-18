// xterm.js and its stylesheet, in a chunk of their own: loaded the first
// time a terminal is shown, never before.
import "@xterm/xterm/css/xterm.css";
export { Terminal } from "@xterm/xterm";
export { FitAddon } from "@xterm/addon-fit";
