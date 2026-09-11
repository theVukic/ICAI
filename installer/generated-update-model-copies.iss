  if FileExists(ExpandConstant('{tmp}\\u2net.onnx')) then CopyFile(ExpandConstant('{tmp}\\u2net.onnx'), ExpandConstant('{app}\\models\\u2net.onnx'), False);
  if FileExists(ExpandConstant('{tmp}\\birefnet-general.onnx')) then CopyFile(ExpandConstant('{tmp}\\birefnet-general.onnx'), ExpandConstant('{app}\\models\\birefnet-general.onnx'), False);
