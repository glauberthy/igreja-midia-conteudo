yt-dlp -f "bv*+ba/b" --merge-output-format mp4 -o "pF21bARMV78_completo.%(ext)s" "https://www.youtube.com/watch?v=pF21bARMV78"

ffmpeg -y \
  -ss 01:35:26.080 \
  -to 01:36:25.080 \
  -i pF21bARMV78_completo.mp4 \
  -vf "scale=-2:1920,crop=1080:1920" \
  -c:v libx264 -preset medium -crf 20 \
  -pix_fmt yuv420p \
  -c:a aac -b:a 192k \
  -movflags +faststart \
  short_pF21bARMV78.mp4