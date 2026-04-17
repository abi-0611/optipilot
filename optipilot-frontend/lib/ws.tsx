"use client";

import { createContext, useContext, useEffect, useState } from "react";

const WS_URL = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080/ws/events";

export interface WsEventEnvelope {
  type: string;
  data: unknown;
  timestamp: string;
}

interface WsContextValue {
  lastMessage: WsEventEnvelope | null;
  isConnected: boolean;
}

const WsContext = createContext<WsContextValue>({
  lastMessage: null,
  isConnected: false,
});

export function WsProvider({ children }: { children: React.ReactNode }) {
  const [lastMessage, setLastMessage] = useState<WsEventEnvelope | null>(null);
  const [isConnected, setIsConnected] = useState(false);

  useEffect(() => {
    let ws: WebSocket | null = null;
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;

    const connect = () => {
      ws = new WebSocket(WS_URL);

      ws.onopen = () => {
        setIsConnected(true);
      };

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as WsEventEnvelope;
          setLastMessage(data);
        } catch (error) {
          console.error("Error parsing WebSocket message", error);
        }
      };

      ws.onclose = () => {
        setIsConnected(false);
        reconnectTimeout = setTimeout(connect, 3000);
      };

      ws.onerror = () => {
        ws?.close();
      };
    };

    connect();

    return () => {
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
      }
      ws?.close();
    };
  }, []);

  return (
    <WsContext.Provider value={{ lastMessage, isConnected }}>
      {children}
    </WsContext.Provider>
  );
}

export function useWsEvents() {
  return useContext(WsContext);
}

