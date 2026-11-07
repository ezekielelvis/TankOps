import { GameEventType } from "./game-event.js";
import {
  GameConfig,
  GameResult,
  TankAction,
  TurnResult,
} from "./game-objects.js";
import { Notifier } from "./notifier.js";
import { Vector } from "./vector.js";

function vectorReviver(key: string, value: any) {
  if (typeof value === "object" && "x" in value && "y" in value) {
    return new Vector(value.x, value.y);
  }
  return value;
}

enum ServerMessageType {
  StartGame = 1,
  TurnResults = 2,
  RoomJoined = 3,
  RoomDisconnected = 4,
  GameFinished = 5,
}

type ServerMessage =
  | {
      type: ServerMessageType.StartGame;
      config: GameConfig;
      roomCode?: string;
      token?: string;
    }
  | {
      type: ServerMessageType.TurnResults;
      turnResults: TurnResult[];
    }
  | {
      type: ServerMessageType.RoomJoined;
    }
  | {
      type: ServerMessageType.RoomDisconnected;
    }
  | {
      type: ServerMessageType.GameFinished;
      result: GameResult;
    };

export enum BotDifficulty {
  Easy = 1,
  Medium = 2,
  Hard = 3,
}

enum ClientMessageType {
  JoinRoom = 1,
  SendTurn = 2,
  QuitRoom = 3,
  Resume = 4,
}

type ClientMessage =
  | {
      type: ClientMessageType.JoinRoom;
      roomCode: string;
      vsBot?: boolean;
      difficulty?: BotDifficulty;
    }
  | {
      type: ClientMessageType.SendTurn;
      actions: TankAction[];
    }
  | {
      type: ClientMessageType.QuitRoom;
    }
  | {
      type: ClientMessageType.Resume;
      roomCode: string;
      token: string;
    };

const RECONNECT_MIN_MS = 500;
const RECONNECT_MAX_MS = 5000;

export class WsDriver {
  open: boolean = false;
  conn!: WebSocket;
  notifier: Notifier;
  private url: string;
  private backoff = RECONNECT_MIN_MS;

  constructor(url: string, notifier: Notifier) {
    this.notifier = notifier;
    this.url = url;
    this.connect();
  }

  // Connects, and keeps retrying with a growing delay whenever the
  // connection drops. WsClose is only reported when an open connection is
  // lost, so failed retries stay silent.
  private connect() {
    this.conn = new WebSocket(this.url);
    this.conn.onopen = () => this.handleOpen();
    this.conn.onclose = () => this.handleClose();
    this.conn.onmessage = (e) => this.handleMessage(e);
  }

  private send(msg: ClientMessage) {
    if (!this.open) {
      return;
    }
    this.conn.send(JSON.stringify(msg));
  }

  public sendStartGame(code: string) {
    const msg: ClientMessage = {
      type: ClientMessageType.JoinRoom,
      roomCode: code,
    };
    this.send(msg);
  }

  public sendStartBotGame(difficulty: BotDifficulty) {
    const msg: ClientMessage = {
      type: ClientMessageType.JoinRoom,
      roomCode: "",
      vsBot: true,
      difficulty: difficulty,
    };
    this.send(msg);
  }

  public sendActions(actions: TankAction[]) {
    const msg: ClientMessage = {
      type: ClientMessageType.SendTurn,
      actions: actions,
    };
    this.send(msg);
  }

  public sendQuitRoom() {
    const msg: ClientMessage = {
      type: ClientMessageType.QuitRoom,
    };
    this.send(msg);
  }

  public sendResume(roomCode: string, token: string) {
    this.send({ type: ClientMessageType.Resume, roomCode, token });
  }

  private handleOpen() {
    this.open = true;
    this.backoff = RECONNECT_MIN_MS;
    this.notifier.notify({ type: GameEventType.WsOpen });
  }

  private handleClose() {
    const wasOpen = this.open;
    this.open = false;
    if (wasOpen) {
      this.notifier.notify({ type: GameEventType.WsClose });
    }
    setTimeout(() => this.connect(), this.backoff);
    this.backoff = Math.min(this.backoff * 2, RECONNECT_MAX_MS);
  }

  private handleMessage(e: MessageEvent) {
    const msg = JSON.parse(e.data, vectorReviver) as ServerMessage;
    switch (msg.type) {
      case ServerMessageType.StartGame:
        this.notifier.notify({
          type: GameEventType.StartGame,
          config: msg.config,
          roomCode: msg.roomCode,
          token: msg.token,
        });
        break;
      case ServerMessageType.TurnResults:
        this.notifier.notify({
          type: GameEventType.ReceiveTurnResults,
          turnResults: msg.turnResults,
        });
        break;
      case ServerMessageType.RoomJoined:
        this.notifier.notify({
          type: GameEventType.RoomJoined,
        });
        break;
      case ServerMessageType.RoomDisconnected:
        this.notifier.notify({
          type: GameEventType.RoomDisconnected,
        });
        break;
      case ServerMessageType.GameFinished:
        this.notifier.notify({
          type: GameEventType.GameFinished,
          result: msg.result,
        });
        break;
    }
  }
}
