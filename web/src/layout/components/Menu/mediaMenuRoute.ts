export type MediaMenuLocation = string | { path: string; query: { nodeId: string } };

export function mediaMenuLocation(path: string, nodeId?: string): MediaMenuLocation {
  if (path.startsWith("/media/") && !["/media/monitoring", "/media/scheduling"].includes(path) && nodeId) {
    return { path, query: { nodeId } };
  }
  return path;
}
