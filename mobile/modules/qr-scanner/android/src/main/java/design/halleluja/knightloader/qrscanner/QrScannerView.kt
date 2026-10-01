package design.halleluja.knightloader.qrscanner

import android.annotation.SuppressLint
import android.content.Context
import android.util.Size
import androidx.camera.core.Camera
import androidx.camera.core.CameraSelector
import androidx.camera.core.FocusMeteringAction
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.core.SurfaceOrientedMeteringPointFactory
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleOwner
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.LuminanceSource
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.ReaderException
import com.google.zxing.common.GlobalHistogramBinarizer
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import expo.modules.kotlin.AppContext
import expo.modules.kotlin.viewevent.EventDispatcher
import expo.modules.kotlin.views.ExpoView
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.min

/** How much of the frame's shorter side the search looks at, around the middle. */
private const val CROP = 0.7

/** How often focus and exposure are measured on the middle again. */
private const val REMETER_MS = 3000L

/**
 * QrScannerView shows the back camera and reads QR codes off its live frames
 * with ZXing, which is free software, where the camera's own scanner would be
 * Google's ML Kit and keep the app out of F-Droid. Focus and exposure are
 * measured on the middle, where the code is held, so a bright screen is
 * exposed for itself rather than for the room around it.
 */
@SuppressLint("ViewConstructor")
class QrScannerView(context: Context, appContext: AppContext) : ExpoView(context, appContext) {
  override val shouldUseAndroidLayout = true

  private val onCode by EventDispatcher<Map<String, String>>()
  private val preview = PreviewView(context).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }
  private val reader = QRCodeReader()
  private val hints = mapOf(DecodeHintType.TRY_HARDER to true)

  // One scan reports one code, however many frames still hold it.
  private val found = AtomicBoolean(false)
  private var provider: ProcessCameraProvider? = null
  private var camera: Camera? = null
  private var analysis: ExecutorService? = null

  // A metering action lapses after a while and the camera goes back to
  // metering the whole frame, so it is renewed for as long as the view shows.
  private val remeter = object : Runnable {
    override fun run() {
      camera?.let(::meterMiddle)
      postDelayed(this, REMETER_MS)
    }
  }

  init {
    addView(preview, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
  }

  override fun onAttachedToWindow() {
    super.onAttachedToWindow()
    val owner = appContext.currentActivity as? LifecycleOwner ?: return
    val future = ProcessCameraProvider.getInstance(context)
    future.addListener({ bind(future.get(), owner) }, ContextCompat.getMainExecutor(context))
  }

  override fun onDetachedFromWindow() {
    removeCallbacks(remeter)
    provider?.unbindAll()
    provider = null
    camera = null
    analysis?.shutdown()
    analysis = null
    super.onDetachedFromWindow()
  }

  private fun bind(cameraProvider: ProcessCameraProvider, owner: LifecycleOwner) {
    if (!isAttachedToWindow) return
    val executor = Executors.newSingleThreadExecutor().also { analysis = it }
    val previewCase = Preview.Builder().build().also { it.surfaceProvider = preview.surfaceProvider }
    // 720 lines resolve a code in the middle of the frame, and a larger frame
    // only makes each search slower.
    val resolution = ResolutionSelector.Builder()
      .setResolutionStrategy(ResolutionStrategy(Size(1280, 720), ResolutionStrategy.FALLBACK_RULE_CLOSEST_HIGHER_THEN_LOWER))
      .build()
    val analysisCase = ImageAnalysis.Builder()
      .setResolutionSelector(resolution)
      .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
      .build()
    analysisCase.setAnalyzer(executor) { image -> image.use(::read) }

    cameraProvider.unbindAll()
    camera = cameraProvider.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, previewCase, analysisCase)
    provider = cameraProvider
    post(remeter)
  }

  private fun meterMiddle(camera: Camera) {
    val point = SurfaceOrientedMeteringPointFactory(1f, 1f).createPoint(0.5f, 0.5f, 0.3f)
    val action = FocusMeteringAction.Builder(point, FocusMeteringAction.FLAG_AF or FocusMeteringAction.FLAG_AE)
      .setAutoCancelDuration(REMETER_MS, TimeUnit.MILLISECONDS)
      .build()
    camera.cameraControl.startFocusAndMetering(action)
  }

  private fun read(image: ImageProxy) {
    if (found.get()) return
    // The first plane is the frame's brightness, all a QR search needs.
    val plane = image.planes[0]
    val bytes = ByteArray(plane.buffer.remaining()).also { plane.buffer.get(it) }
    val side = (min(image.width, image.height) * CROP).toInt()
    val source = PlanarYUVLuminanceSource(
      bytes, plane.rowStride, image.height,
      (image.width - side) / 2, (image.height - side) / 2, side, side, false,
    )
    val text = decode(source) ?: return
    if (found.compareAndSet(false, true)) post { onCode(mapOf("data" to text)) }
  }

  // HybridBinarizer judges each patch against its neighbours and handles
  // uneven light; the global one catches a washed-out code whose patches all
  // look the same colour to it.
  private fun decode(source: LuminanceSource): String? {
    for (bitmap in listOf(BinaryBitmap(HybridBinarizer(source)), BinaryBitmap(GlobalHistogramBinarizer(source)))) {
      try {
        return reader.decode(bitmap, hints).text
      } catch (_: ReaderException) {
      } finally {
        reader.reset()
      }
    }
    return null
  }
}
