// jsdom 30 的 Blob/File 仍未实现 arrayBuffer()/text()（2026-11-12 实测
// `File.prototype.arrayBuffer === undefined`，send-media.test.js 的代理分支断言
// `res.file.arrayBuffer()` 因此必败）。产品代码运行在真浏览器里，不动；
// 这里用 jsdom 已实现的 FileReader 把两个方法补上，view 层零改动。
//
// 口径：只在缺失时补（真浏览器环境/未来 jsdom 补齐后本文件自动变空操作）。
if (typeof Blob !== 'undefined' && typeof Blob.prototype.arrayBuffer !== 'function') {
  function blobToBuffer(blob, readerMethod) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.onerror = () => reject(reader.error || new Error('FileReader failed'));
      reader[readerMethod](blob);
    });
  }
  Blob.prototype.arrayBuffer = function arrayBuffer() {
    return blobToBuffer(this, 'readAsArrayBuffer');
  };
  Blob.prototype.text = function text() {
    return blobToBuffer(this, 'readAsText').then(String);
  };
}
