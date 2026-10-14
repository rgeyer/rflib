import { notifications } from "@mantine/notifications";
import { message } from "../api/client";

export function ok(text: string) {
  notifications.show({ message: text, color: "green" });
}

export function failed(err: unknown) {
  notifications.show({ title: "That didn't work", message: message(err), color: "red", autoClose: 8000 });
}

export function warn(lines: string[]) {
  for (const line of lines) notifications.show({ message: line, color: "yellow", autoClose: 10000 });
}
