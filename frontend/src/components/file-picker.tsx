import { FileArrowUpIcon } from "@phosphor-icons/react";
import { type DragEvent, useId, useState } from "react";
import { Dot } from "@/components/dot";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";

export function FilePicker({
  accept,
  file,
  detail,
  disabled,
  onFile,
}: {
  accept: string;
  file: File | null;
  detail?: string;
  disabled?: boolean;
  onFile: (file: File) => void;
}) {
  const id = useId();
  const [dragging, setDragging] = useState(false);

  function handleDrop(e: DragEvent<HTMLLabelElement>) {
    e.preventDefault();
    setDragging(false);
    const dropped = e.dataTransfer.files[0];
    if (dropped && !disabled) onFile(dropped);
  }

  return (
    <div className="flex flex-col gap-2">
      <label
        htmlFor={id}
        onDragOver={(e) => {
          e.preventDefault();
          if (!disabled) setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={handleDrop}
        className={cn(
          "flex cursor-pointer flex-col items-center gap-2 border-2 border-dashed border-border/50 px-4 py-8 text-center font-mono text-sm transition-colors hover:border-foreground hover:bg-muted",
          "has-focus-visible:border-foreground has-focus-visible:bg-muted",
          dragging && "border-foreground bg-muted",
          disabled && "pointer-events-none opacity-40",
        )}
      >
        <FileArrowUpIcon size={28} />
        <span className="font-semibold">{file ? "choose another file" : "choose a file"}</span>
        <span className="text-muted-foreground">or drop one here — it stays on your device</span>
      </label>
      <input
        id={id}
        type="file"
        accept={accept}
        disabled={disabled}
        className="sr-only"
        onChange={(e) => {
          const picked = e.target.files?.[0];
          if (picked) onFile(picked);
          e.target.value = ""; // so picking the same file again still fires
        }}
      />
      {file && (
        <p className="truncate text-sm">
          {file.name}
          <span className="text-muted-foreground">
            <Dot />
            {formatBytes(file.size)}
            {detail && (
              <>
                <Dot />
                {detail}
              </>
            )}
          </span>
        </p>
      )}
    </div>
  );
}
