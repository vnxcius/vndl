export const imageFormats = {
  jpg: { mime: "image/jpeg", lossy: true },
  png: { mime: "image/png", lossy: false },
  webp: { mime: "image/webp", lossy: true },
} as const;

export type ImageFormat = keyof typeof imageFormats;

export interface DecodedImage {
  image: HTMLImageElement;
  width: number;
  height: number;
}

// An <img> rather than createImageBitmap: it decodes everything the browser
// can display (SVG included) and applies EXIF orientation, so phone photos
// come out upright.
export async function decodeImage(file: File): Promise<DecodedImage> {
  const url = URL.createObjectURL(file);
  try {
    const image = new Image();
    image.src = url;
    await image.decode();
    if (!image.naturalWidth || !image.naturalHeight) throw new Error();
    return { image, width: image.naturalWidth, height: image.naturalHeight };
  } catch {
    throw new Error("your browser can't open this image");
  } finally {
    URL.revokeObjectURL(url); // decode() has already read it
  }
}

export async function convertImage(
  { image, width, height }: DecodedImage,
  format: ImageFormat,
  quality: number,
): Promise<Blob> {
  const { mime, lossy } = imageFormats[format];
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("couldn't start the converter — try a smaller image");

  // JPG has no transparency; without a fill, transparent pixels turn black.
  if (format === "jpg") {
    ctx.fillStyle = "#fff";
    ctx.fillRect(0, 0, width, height);
  }
  ctx.drawImage(image, 0, 0, width, height);

  const blob = await new Promise<Blob | null>((resolve) =>
    canvas.toBlob(resolve, mime, lossy ? quality : undefined),
  );
  // Over the browser's canvas size limit, toBlob yields null.
  if (!blob) throw new Error("this image is too large for your browser to convert");
  // A browser that can't encode the format silently falls back to PNG.
  if (blob.type !== mime) throw new Error(`your browser can't save ${format} images`);
  return blob;
}
