// Images in the settings of a template (logos, a start image …): the server
// gives them as their address and their size in pixels.

/** The value of an image setting: its address and size in pixels. */
export interface ImageSetting { src: string; width: number; height: number }

/** The image of a setting, if one is set (null otherwise): only files of the
 * wiki, with a size. */
export function imageSetting(v: unknown): ImageSetting | null {
  if (!v || typeof v !== 'object') return null;
  const x = v as Partial<ImageSetting>;
  return typeof x.src === 'string' && x.src.startsWith('/api/v1/files/') && Number(x.width) > 0 && Number(x.height) > 0
    ? { src: x.src, width: Number(x.width), height: Number(x.height) } : null;
}
