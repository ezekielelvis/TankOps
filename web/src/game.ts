import {
  GameConfig,
  GameResult,
  GameState,
  TurnResult,
  TurnResultType,
} from "./game-objects.js";
import { Vector } from "./vector.js";
import { DisplayDriver } from "./display-driver.js";
import { Grid } from "./grid.js";
import { UI, UIMode } from "./ui.js";
import { Notifier } from "./notifier.js";
import { GameEvent, GameEventType } from "./game-event.js";
import { BotDifficulty, WsDriver } from "./ws-driver.js";
import { AudioDriver } from "./audio-driver.js";

const WS_URL = "ws";
const STORAGE_SOUND = "tankops.sound";
const STORAGE_TUTORIAL = "tankops.tutorialSeen";

const HELP_PAGES = [
  "HOW TO PLAY (1/3)\n\nEach turn give every tank\none order: move or fire.\nWhen both players send,\nthe turn plays out.",
  "HOW TO PLAY (2/3)\n\nMOVE: press a tank and\ndrag along the hexes.\nFIRE: hold on a tank,\nthen drag to aim.",
  "HOW TO PLAY (3/3)\n\nKEYS: tab picks a tank,\nq e a d z c steps or aims,\nf fires, backspace clears,\nenter sends, m mutes.\nThe outer ring collapses!",
];

function storageGet(key: string): string | null {
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function storageSet(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // storage can be unavailable (private mode); the setting just won't stick
  }
}

// hex direction for each keyboard key; indexes match unitVectorToIdx
const KEY_DIRECTIONS: { [key: string]: number } = {
  d: 0,
  c: 1,
  z: 2,
  a: 3,
  q: 4,
  e: 5,
};

const PINCH_ZOOM_IN = 1.3;
const PINCH_ZOOM_OUT = 1 / 1.3;
const AUDIO_FAIL_MESSAGE = "failed to load audio";

function resultString(result: GameResult): string {
  switch (result) {
    case GameResult.Win:
      return "you won!";
    case GameResult.Draw:
      return "draw";
    case GameResult.Lose:
      return "you lost...";
  }
}

function elementToScreenCoords(elementP: Vector): Vector {
  return elementP.mul(window.devicePixelRatio).round();
}

enum Layer {
  UI,
  Grid,
}

export class Game {
  audioDriver: AudioDriver;
  notifier: Notifier;
  displayDriver: DisplayDriver;
  wsDriver: WsDriver;
  grid: Grid | null = null;
  ui: UI;
  isPointerDown = false;
  layer: Layer = Layer.UI;
  freeze: boolean = false;
  // lets a dropped connection resume the running game
  private _session: { roomCode: string; token: string } | null = null;
  announcer: HTMLElement;
  pointers: Map<number, Vector> = new Map();
  pinchBase: number | null = null;
  states: {
    mainMenu: StateGame;
    waitingForRoom: StateGame;
    waitingRoom: StateGame;
    inGame: StateGame;
  };
  state: StateGame;

  constructor(ctx: CanvasRenderingContext2D) {
    this.notifier = new Notifier(this);
    this.audioDriver = new AudioDriver(this.notifier);
    this.wsDriver = new WsDriver(WS_URL, this.notifier);
    const canvas = ctx.canvas;
    this.initEventListeners(canvas);
    this.announcer = this.createAnnouncer(canvas);
    this.initKeyboard();

    const inputElement = this.createInputElement();
    ctx.canvas.parentNode?.insertBefore(inputElement, null);
    this.ui = new UI(this.notifier, inputElement);
    this.displayDriver = new DisplayDriver(ctx, null, this.ui);

    window.addEventListener("resize", () => {
      this.resize();
    });
    this.resize();

    this.states = {
      mainMenu: new GameStateMainMenu(this),
      waitingForRoom: new GameStateWaitForRoom(this),
      waitingRoom: new GameStateWaitingRoom(this),
      inGame: new GameStateInGame(this),
    };
    this.state = this.states.mainMenu;

    if (storageGet(STORAGE_SOUND) === "on") {
      this.ui.setAudioButton(true);
      this.audioDriver.setSoundInGame(true);
    }
  }

  // A visually hidden live region that mirrors what happens on the canvas.
  // The session also lives in sessionStorage so a page refresh can resume.
  get session(): { roomCode: string; token: string } | null {
    return this._session;
  }

  set session(value: { roomCode: string; token: string } | null) {
    this._session = value;
    try {
      if (value === null) {
        window.sessionStorage.removeItem("tankops.session");
      } else {
        window.sessionStorage.setItem("tankops.session", JSON.stringify(value));
      }
    } catch {
      // not fatal: resume after a refresh just won't work
    }
  }

  public loadStoredSession(): { roomCode: string; token: string } | null {
    try {
      const raw = window.sessionStorage.getItem("tankops.session");
      if (raw === null) return null;
      const v = JSON.parse(raw);
      if (typeof v.roomCode === "string" && typeof v.token === "string") {
        return v;
      }
    } catch {
      // fall through
    }
    return null;
  }

  private createAnnouncer(canvas: HTMLCanvasElement): HTMLElement {
    canvas.setAttribute("role", "img");
    canvas.setAttribute(
      "aria-label",
      "tank-ops game board. Keyboard: tab selects a tank, q e a d z c move or aim, f fires, enter sends the turn.",
    );
    const el = document.createElement("div");
    el.setAttribute("role", "status");
    el.setAttribute("aria-live", "polite");
    el.style.cssText =
      "position:absolute;left:-9999px;width:1px;height:1px;overflow:hidden;";
    canvas.parentNode?.insertBefore(el, null);
    return el;
  }

  public announce(text: string) {
    if (text === "") return;
    this.announcer.textContent = text;
  }

  public showHelp() {
    for (const page of HELP_PAGES) {
      this.ui.addModal(page);
    }
  }

  public showTutorialOnce() {
    if (storageGet(STORAGE_TUTORIAL) === "yes") return;
    storageSet(STORAGE_TUTORIAL, "yes");
    this.showHelp();
  }

  public setSound(on: boolean) {
    this.ui.setAudioButton(on);
    this.audioDriver.setSoundInGame(on);
    storageSet(STORAGE_SOUND, on ? "on" : "off");
  }

  private initKeyboard() {
    window.addEventListener("keydown", (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement) return;
      if (e.ctrlKey || e.metaKey || e.altKey) return;
      if (this.handleKey(e)) {
        e.preventDefault();
      }
    });
  }

  // returns true if the key was used
  private handleKey(e: KeyboardEvent): boolean {
    const key = e.key.length === 1 ? e.key.toLowerCase() : e.key;
    if (this.ui.hasModal()) {
      if (key === "Enter" || key === "Escape" || key === " ") {
        this.ui.removeModal();
        return true;
      }
      return false;
    }
    if (key === "h" || key === "?") {
      this.showHelp();
      return true;
    }
    if (this.state !== this.states.inGame || this.grid === null) return false;

    const pan = 60;
    switch (key) {
      case "ArrowLeft":
        this.displayDriver.addCameraOffset(new Vector(pan, 0));
        return true;
      case "ArrowRight":
        this.displayDriver.addCameraOffset(new Vector(-pan, 0));
        return true;
      case "ArrowUp":
        this.displayDriver.addCameraOffset(new Vector(0, pan));
        return true;
      case "ArrowDown":
        this.displayDriver.addCameraOffset(new Vector(0, -pan));
        return true;
      case "+":
      case "=":
        this.handleZoomIn();
        return true;
      case "-":
        this.handleZoomOut();
        return true;
      case "Tab":
        this.announce(this.grid.keyboardSelectTank(e.shiftKey ? -1 : 1));
        return true;
      case "f":
        this.announce(this.grid.keyboardToggleFire());
        return true;
      case "Backspace":
      case "Delete":
        this.announce(this.grid.keyboardClearOrders());
        return true;
      case "Enter":
        if (this.ui.isSendTurnEnabled()) {
          this.notifier.notify({ type: GameEventType.ButtonSendTurn });
          this.announce("turn sent");
        }
        return true;
      case "m":
        if (this.ui.isSoundOn()) {
          this.notifier.notify({ type: GameEventType.ButtonMute });
        } else if (this.ui.isUnmuteAllowed()) {
          this.notifier.notify({ type: GameEventType.ButtonUnmute });
        }
        return true;
    }
    if (key in KEY_DIRECTIONS) {
      this.announce(this.grid.keyboardDirection(KEY_DIRECTIONS[key]));
      return true;
    }
    return false;
  }

  public summarize(results: TurnResult[]): string {
    const own = new Set(this.grid?.gameState.playerTanks.map((t) => t.id));
    const parts: string[] = [];
    for (const r of results) {
      switch (r.type) {
        case TurnResultType.Explosion:
          if (r.destroyed) {
            parts.push(
              own.has(r.id)
                ? `your tank ${r.id} was destroyed`
                : `enemy tank ${r.id} was destroyed`,
            );
          }
          break;
        case TurnResultType.Destroyed:
          parts.push(`enemy tank ${r.id} was destroyed`);
          break;
        case TurnResultType.Visible:
          if (r.visible) parts.push(`enemy tank ${r.id} spotted`);
          break;
        case TurnResultType.Shrink:
          if (!r.started) parts.push("the outer ring collapses next turn");
          break;
      }
    }
    return parts.length > 0 ? parts.join(". ") : "turn played";
  }

  public update(event: GameEvent) {
    this.state.update(event);
  }

  public setState(state: StateGame) {
    this.state = state;
    this.state.onEnter();
  }

  public run() {
    this.draw(0);
  }

  private initEventListeners(canvas: HTMLCanvasElement) {
    canvas.addEventListener("pointerdown", (e: PointerEvent) => {
      const screenP = elementToScreenCoords(new Vector(e.offsetX, e.offsetY));
      this.audioDriver.unlock();
      this.pointers.set(e.pointerId, screenP);
      if (this.pointers.size === 2) {
        // a second finger turns the gesture into a pinch
        this.grid?.handlePointerEnd(screenP);
        this.isPointerDown = false;
        this.pinchBase = this.pointerSpread();
        return;
      }
      if (this.pointers.size > 2) return;
      this.handlePointerStart(screenP);
    });
    const finish = (e: PointerEvent) => {
      const screenP = elementToScreenCoords(new Vector(e.offsetX, e.offsetY));
      const wasPinching = this.pointers.size >= 2;
      this.pointers.delete(e.pointerId);
      if (wasPinching) {
        if (this.pointers.size < 2) this.pinchBase = null;
        return;
      }
      this.handlePointerEnd(screenP);
    };
    canvas.addEventListener("pointerup", finish);
    canvas.addEventListener("pointercancel", finish);
    canvas.addEventListener("pointermove", (e: PointerEvent) => {
      const screenP = elementToScreenCoords(new Vector(e.offsetX, e.offsetY));
      if (this.pointers.has(e.pointerId)) {
        this.pointers.set(e.pointerId, screenP);
      }
      if (this.pointers.size >= 2) {
        this.handlePinch();
        return;
      }
      this.handlePointerMove(screenP);
    });
    canvas.addEventListener("wheel", (e: WheelEvent) => {
      if (e.deltaY > 0) {
        this.handleZoomOut();
        return;
      }
      this.handleZoomIn();
    });
  }

  private pointerSpread(): number {
    const pts = [...this.pointers.values()];
    if (pts.length < 2) return 0;
    return pts[0].sub(pts[1]).length();
  }

  // Zoom has discrete levels, so step once each time the fingers have moved
  // far enough from where the last step happened.
  private handlePinch() {
    const spread = this.pointerSpread();
    if (this.pinchBase === null || this.pinchBase === 0) {
      this.pinchBase = spread;
      return;
    }
    const ratio = spread / this.pinchBase;
    if (ratio > PINCH_ZOOM_IN) {
      this.handleZoomIn();
      this.pinchBase = spread;
    } else if (ratio < PINCH_ZOOM_OUT) {
      this.handleZoomOut();
      this.pinchBase = spread;
    }
  }

  private createInputElement(): HTMLInputElement {
    const e = document.createElement("input");
    e.setAttribute("id", "in-game-input");
    e.setAttribute("type", "text");
    e.setAttribute("placeholder", "code");
    return e;
  }

  public initGrid(config: GameConfig) {
    const gameState = new GameState(config);
    this.grid = new Grid(
      gameState,
      this.displayDriver,
      config,
      this.notifier,
      this.audioDriver,
    );
    this.displayDriver.gameState = gameState;
    this.displayDriver.reset(config.center);
  }

  public removeGrid() {
    this.session = null;
    this.grid = null;
    this.displayDriver.gameState = null;
  }

  public handleZoomIn() {
    this.displayDriver.handleZoomIn();
  }

  public handleZoomOut() {
    this.displayDriver.handleZoomOut();
  }

  private handlePointerStart(p: Vector) {
    this.isPointerDown = true;
    this.layer = this.ui.collides(p) ? Layer.UI : Layer.Grid;
    if (this.layer === Layer.UI) {
      this.ui.handlePointerStart(p);
    } else {
      this.grid?.handlePointerStart(p);
    }
  }

  private handlePointerEnd(p: Vector) {
    this.isPointerDown = false;
    if (this.layer === Layer.UI) {
      this.ui.handlePointerEnd(p);
    } else {
      this.grid?.handlePointerEnd(p);
    }
    this.layer = Layer.UI;
  }

  private handlePointerMove(p: Vector) {
    if (!this.isPointerDown) return;
    if (this.layer === Layer.UI) {
      this.ui.handlePointerMove(p);
    } else {
      this.grid?.handlePointerMove(p);
    }
  }

  private draw(curT: number) {
    this.displayDriver.draw(this.freeze);
    this.grid?.setT(curT);
    this.grid?.tick();
    requestAnimationFrame((t: number) => {
      this.draw(t);
    });
  }

  private resize() {
    this.displayDriver.resize();
  }
}

interface StateGame {
  str: string;
  update(event: GameEvent): void;
  onEnter(): void;
}

class GameStateMainMenu implements StateGame {
  str = "main-menu";
  game: Game;
  constructor(game: Game) {
    this.game = game;
  }

  onEnter(): void {
    this.game.audioDriver.setSoundGlobal(false);
  }

  update(event: GameEvent) {
    switch (event.type) {
      case GameEventType.WsOpen:
        this.game.ui.setOnlineGameAvailability(true);
        const stored = this.game.loadStoredSession();
        if (stored !== null) {
          // the page was refreshed during a game: try to get back into it
          this.game.wsDriver.sendResume(stored.roomCode, stored.token);
          this.game.freeze = true;
          this.game.setState(this.game.states.waitingForRoom);
        }
        break;
      case GameEventType.WsClose:
        this.game.freeze = false;
        this.game.ui.setOnlineGameAvailability(false);
        this.game.ui.addModal("connection lost");
        break;
      case GameEventType.ButtonJoinRoom:
        const code = this.game.ui.getRoomCode();
        this.game.wsDriver.sendStartGame(code);
        this.game.freeze = true;
        this.game.setState(this.game.states.waitingForRoom);
        break;
      case GameEventType.ButtonHelp:
        this.game.showHelp();
        break;
      case GameEventType.ButtonPlayEasy:
        this.startBotGame(BotDifficulty.Easy);
        break;
      case GameEventType.ButtonPlayMedium:
        this.startBotGame(BotDifficulty.Medium);
        break;
      case GameEventType.ButtonPlayHard:
        this.startBotGame(BotDifficulty.Hard);
        break;
      case GameEventType.AudioLoadFail:
        this.game.ui.addModal(AUDIO_FAIL_MESSAGE);
        break;
      case GameEventType.AudioLoadSuccess:
        this.game.ui.allowUnmute();
        break;
    }
  }

  private startBotGame(difficulty: BotDifficulty) {
    this.game.wsDriver.sendStartBotGame(difficulty);
    this.game.freeze = true;
    this.game.setState(this.game.states.waitingForRoom);
  }
}

class GameStateWaitForRoom implements StateGame {
  str = "wait-for-room";
  game: Game;
  constructor(game: Game) {
    this.game = game;
  }

  onEnter(): void {
    this.game.audioDriver.setSoundGlobal(false);
  }

  update(event: GameEvent): void {
    switch (event.type) {
      case GameEventType.WsOpen:
        this.game.ui.setOnlineGameAvailability(true);
        break;
      case GameEventType.WsClose:
        this.game.freeze = false;
        this.game.ui.setOnlineGameAvailability(false);
        this.game.ui.addModal("connection lost");
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.RoomJoined:
        this.game.freeze = false;
        this.game.ui.enableMode(UIMode.WaitingRoom);
        this.game.setState(this.game.states.waitingRoom);
        break;
      case GameEventType.RoomDisconnected:
        this.game.freeze = false;
        this.game.session = null;
        this.game.ui.addModal("cant join room");
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.AudioLoadFail:
        this.game.ui.addModal(AUDIO_FAIL_MESSAGE);
        break;
      case GameEventType.AudioLoadSuccess:
        this.game.ui.allowUnmute();
        break;
    }
  }
}
class GameStateWaitingRoom implements StateGame {
  str = "waiting-room";
  game: Game;
  constructor(game: Game) {
    this.game = game;
  }

  onEnter(): void {
    this.game.audioDriver.setSoundGlobal(false);
  }

  update(event: GameEvent): void {
    switch (event.type) {
      case GameEventType.WsOpen:
        this.game.ui.setOnlineGameAvailability(true);
        break;
      case GameEventType.WsClose:
        this.game.ui.setOnlineGameAvailability(false);
        this.game.ui.addModal("connection lost");
        this.game.ui.enableMode(UIMode.Main);
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.StartGame:
        this.game.initGrid(event.config);
        if (event.roomCode && event.token) {
          this.game.session = { roomCode: event.roomCode, token: event.token };
        }
        this.game.ui.enableMode(UIMode.InGame);
        this.game.setState(this.game.states.inGame);
        this.game.showTutorialOnce();
        break;
      case GameEventType.RoomDisconnected:
        this.game.ui.addModal("room disconnected");
        this.game.ui.enableMode(UIMode.Main);
        this.game.setState(this.game.states.mainMenu);
        this.game.freeze;
        break;
      case GameEventType.ButtonQuitGame:
        this.game.wsDriver.sendQuitRoom();
        this.game.ui.enableMode(UIMode.Main);
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.AudioLoadFail:
        this.game.ui.addModal(AUDIO_FAIL_MESSAGE);
        break;
      case GameEventType.AudioLoadSuccess:
        this.game.ui.allowUnmute();
        break;
    }
  }
}
class GameStateInGame implements StateGame {
  game: Game;
  isAnimating: boolean = false;

  str = "in-game";
  modalQueue: string[] = [];
  gameFinished: boolean = false;
  counterIncoming: number = 0;
  counterFinished: number = 0;

  constructor(game: Game) {
    this.game = game;
  }

  onEnter() {
    this.game.audioDriver.setSoundGlobal(true);
    this.isAnimating = false;
    this.modalQueue = [];
    this.gameFinished = false;
    this.counterIncoming = 0;
    this.counterFinished = 0;

    this.game.ui.setSendTurnAvailability(true);
  }

  update(event: GameEvent): void {
    switch (event.type) {
      case GameEventType.ReceiveTurnResults:
        this.counterIncoming++;
        this.game.announce(this.game.summarize(event.turnResults));
        this.game.grid?.pushResults(event.turnResults);
        this.game.ui.setSendTurnAvailability(false);
        this.isAnimating = true;
        break;
      case GameEventType.GameFinished:
        this.gameFinished = true;
        this.game.session = null;
        this.game.announce(`game over, ${resultString(event.result)}`);
        this.game.ui.setSendTurnAvailability(false);
        if (this.isAnimating) {
          this.modalQueue.push(resultString(event.result));
          return;
        }
        this.game.ui.addModal(resultString(event.result));
        break;
      case GameEventType.WsClose:
        if (this.game.session !== null && !this.gameFinished) {
          // keep the board and try to resume once the socket is back
          this.game.freeze = true;
          this.game.ui.setSendTurnAvailability(false);
          this.game.announce("connection lost, reconnecting");
          break;
        }
        this.game.ui.setSendTurnAvailability(false);
        this.game.ui.setOnlineGameAvailability(false);
        this.game.ui.addModal("server disconnected");
        this.game.removeGrid();
        this.game.ui.enableMode(UIMode.Main);
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.RoomDisconnected:
        this.game.freeze = false;
        this.game.ui.setSendTurnAvailability(false);
        if (this.gameFinished) {
          return;
        }
        if (this.isAnimating) {
          this.modalQueue.push("room disconnected");
          return;
        }
        this.game.ui.addModal("room disconnected");
        this.game.removeGrid();
        this.game.ui.enableMode(UIMode.Main);
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.WsOpen:
        if (this.game.session !== null && !this.gameFinished) {
          this.game.wsDriver.sendResume(
            this.game.session.roomCode,
            this.game.session.token,
          );
        }
        break;
      case GameEventType.StartGame:
        // the server resumed us: it sends the start again, then every turn
        // played so far, which replay as normal animations
        this.game.initGrid(event.config);
        this.isAnimating = false;
        this.counterIncoming = 0;
        this.counterFinished = 0;
        this.game.freeze = false;
        this.game.ui.setSendTurnAvailability(true);
        this.game.announce("reconnected");
        break;
      case GameEventType.ButtonHelp:
        this.game.showHelp();
        break;
      case GameEventType.ButtonZoomIn:
        this.game.handleZoomIn();
        break;
      case GameEventType.ButtonZoomOut:
        this.game.handleZoomOut();
        break;
      case GameEventType.ButtonQuitGame:
        if (!this.gameFinished) {
          this.game.wsDriver.sendQuitRoom();
        }
        this.game.removeGrid();
        this.game.ui.enableMode(UIMode.Main);
        this.game.setState(this.game.states.mainMenu);
        break;
      case GameEventType.ButtonSendTurn:
        if (this.game.grid === null) return;
        this.game.ui.setSendTurnAvailability(false);
        const actions = this.game.grid.getActions();
        this.game.wsDriver.sendActions(actions);
        break;
      case GameEventType.AnimationEnd:
        this.counterFinished++;
        if (this.counterFinished === this.counterIncoming) {
          this.isAnimating = false;
          this.game.ui.setSendTurnAvailability(!this.gameFinished);
          for (const modalText of this.modalQueue) {
            this.game.ui.addModal(modalText);
          }
        }
        break;
      case GameEventType.TankManipulation:
        if (!this.gameFinished && !this.isAnimating) {
          this.game.ui.setSendTurnAvailability(true);
        }
        break;
      case GameEventType.AudioLoadFail:
        this.game.ui.addModal(AUDIO_FAIL_MESSAGE);
        break;
      case GameEventType.AudioLoadSuccess:
        this.game.ui.allowUnmute();
        break;
      case GameEventType.ButtonUnmute:
        this.game.setSound(true);
        break;
      case GameEventType.ButtonMute:
        this.game.setSound(false);
        break;
    }
  }
}
