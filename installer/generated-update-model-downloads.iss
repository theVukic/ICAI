  if CompareText(GetSHA256OfFile(ExpandConstant('{app}\\models\\u2net.onnx')), '8d10d2f3bb75ae3b6d527c77944fc5e7dcd94b29809d47a739a7a728a912b491') <> 0 then begin
    DownloadPage.Add('https://github.com/theVukic/ASI-Releases/releases/download/v0.0.1/u2net.onnx', 'u2net.onnx', '8d10d2f3bb75ae3b6d527c77944fc5e7dcd94b29809d47a739a7a728a912b491');
    ModelDownloadCount := ModelDownloadCount + 1;
  end;
  if CompareText(GetSHA256OfFile(ExpandConstant('{app}\\models\\birefnet-general.onnx')), '58f621f00f5d756097615970a88a791584600dcf7c45b18a0a6267535a1ebd3c') <> 0 then begin
    DownloadPage.Add('https://github.com/theVukic/ASI-Releases/releases/download/v0.0.1/birefnet-general.onnx', 'birefnet-general.onnx', '58f621f00f5d756097615970a88a791584600dcf7c45b18a0a6267535a1ebd3c');
    ModelDownloadCount := ModelDownloadCount + 1;
  end;
  if CompareText(GetSHA256OfFile(ExpandConstant('{app}\\models\\Qwen3-4B-Q4_K_M.gguf')), '7485fe6f11af29433bc51cab58009521f205840f5b4ae3a32fa7f92e8534fdf5') <> 0 then begin
    DownloadPage.Add('https://github.com/theVukic/ASI-Releases/releases/download/v0.0.1/Qwen3-4B-Q4_K_M.part001', 'Qwen3-4B-Q4_K_M.part001', 'b1078ae348590b78c1c62be6f23238821a59d11772177e6f8666b8cd8ee691ad');
    ModelDownloadCount := ModelDownloadCount + 1;
    DownloadPage.Add('https://github.com/theVukic/ASI-Releases/releases/download/v0.0.1/Qwen3-4B-Q4_K_M.part002', 'Qwen3-4B-Q4_K_M.part002', 'beef59f3d20bc5a0a3b7743ce09735cc11a3078a338687bf9fc601e617031c86');
    ModelDownloadCount := ModelDownloadCount + 1;
  end;
