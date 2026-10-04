import { useCallback, useEffect, useRef, useState } from "react";

export interface ConvertedFile {
  blob: Blob;
  name: string;
  url: string;
}

// Holds a conversion result with an object URL to save it from. The URL is
// revoked when the result is replaced or on unmount — not right after a
// download starts, which can cut that download short.
export function useConvertedFile() {
  const [result, setResult] = useState<ConvertedFile | null>(null);
  const urlRef = useRef<string | null>(null);

  useEffect(
    () => () => {
      if (urlRef.current) URL.revokeObjectURL(urlRef.current);
    },
    [],
  );

  const set = useCallback((next: { blob: Blob; name: string } | null): ConvertedFile | null => {
    if (urlRef.current) URL.revokeObjectURL(urlRef.current);
    const converted = next && { ...next, url: URL.createObjectURL(next.blob) };
    urlRef.current = converted?.url ?? null;
    setResult(converted);
    return converted;
  }, []);

  return [result, set] as const;
}
