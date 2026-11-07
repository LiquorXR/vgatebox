// 纵向下滑关闭抽屉的释放判定：位移达阈值，或短促快甩，二者居一。
// 纯函数，方便单测；阈值按手机拇指操作调参。
export function shouldDismissDrawer(dyPx: number, dtMs: number): boolean {
  if (dyPx >= 120) return true;
  if (dtMs <= 0) return false;
  return dyPx >= 24 && dyPx / (dtMs / 1000) > 500;
}
