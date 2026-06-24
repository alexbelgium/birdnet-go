export type PinchEndCallback = (scale: number, midX: number, midY: number) => void;

function getTouchDistance(touches: TouchList): number {
  const t0 = touches.item(0);
  const t1 = touches.item(1);
  if (t0 === null || t1 === null) return 0;
  const dx = t1.clientX - t0.clientX;
  const dy = t1.clientY - t0.clientY;
  return Math.sqrt(dx * dx + dy * dy);
}

function getTouchMidpoint(touches: TouchList): { x: number; y: number } {
  const t0 = touches.item(0);
  const t1 = touches.item(1);
  if (t0 === null || t1 === null) return { x: 0, y: 0 };
  return {
    x: (t0.clientX + t1.clientX) / 2,
    y: (t0.clientY + t1.clientY) / 2,
  };
}

/**
 * Creates a pinch gesture detector that calls onPinchEnd with the final
 * scale factor and midpoint coordinates when the user lifts both fingers.
 * Returns { attach } where attach(element, cb) installs listeners and
 * returns a cleanup function.
 */
export function createPinchDetector() {
  function attach(element: HTMLElement, onPinchEnd: PinchEndCallback): () => void {
    let startDist = 0;
    let currentDist = 0;
    let startMid = { x: 0, y: 0 };
    let isPinching = false;

    function onTouchStart(e: TouchEvent): void {
      if (e.touches.length === 2) {
        startDist = getTouchDistance(e.touches);
        currentDist = startDist;
        startMid = getTouchMidpoint(e.touches);
        isPinching = true;
      } else {
        isPinching = false;
      }
    }

    function onTouchMove(e: TouchEvent): void {
      if (isPinching && e.touches.length === 2) {
        currentDist = getTouchDistance(e.touches);
      }
    }

    function onTouchEnd(_e: TouchEvent): void {
      if (isPinching && startDist > 0) {
        onPinchEnd(currentDist / startDist, startMid.x, startMid.y);
      }
      isPinching = false;
      startDist = 0;
    }

    element.addEventListener('touchstart', onTouchStart, { passive: true });
    element.addEventListener('touchmove', onTouchMove, { passive: true });
    element.addEventListener('touchend', onTouchEnd, { passive: true });

    return () => {
      element.removeEventListener('touchstart', onTouchStart);
      element.removeEventListener('touchmove', onTouchMove);
      element.removeEventListener('touchend', onTouchEnd);
    };
  }

  return { attach };
}
