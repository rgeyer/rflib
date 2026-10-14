import { useParams } from "react-router-dom";

/** useLib is the library in the URL (/l/:lib/...). */
export function useLib(): string {
  return useParams().lib ?? "";
}

/** libPath encodes a library name for API paths and links. */
export function enc(s: string): string {
  return encodeURIComponent(s);
}
