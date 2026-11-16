import { useQueryClient } from "@tanstack/vue-query";
import { ApiError } from "@/api/client";
import { keys } from "@/api/queries";
import { toast, toastError } from "@/lib/toast";

/**
 * Shared error handling for the access tabs: refresh the affected queries and
 * keep 5xx responses visible instead of leaving the page on a stale state.
 */
export function useAccessError() {
  const queryClient = useQueryClient();
  return (error: unknown, fallback: string) => {
    void queryClient.invalidateQueries({ queryKey: keys.exits });
    void queryClient.invalidateQueries({ queryKey: keys.socks });
    void queryClient.invalidateQueries({ queryKey: keys.thirdParty });
    if (error instanceof ApiError && error.status >= 500) {
      toast(`操作未完成（HTTP ${error.status}）。已刷新当前状态，请确认是否生效后再重试。`, {
        tone: "error",
        duration: 9000,
      });
      return;
    }
    toastError(error, fallback);
  };
}
