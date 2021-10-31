var ZipPlugin = require('zip-webpack-plugin');
const path = require('path');
const SRC_DIR = path.resolve(__dirname, 'src');
const OUT_DIR = path.resolve(__dirname, 'dist');

module.exports = {
  entry: [path.resolve(SRC_DIR, 'index.js')],
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        use: 'ts-loader',
        exclude: /node_modules/,
      },
    ],
  },
  resolve: {
    modules: ['node_modules'],
    extensions: [ '.tsx', '.ts', '.js', '.json' ],
  },
  target: 'node',
  externals: [
    'aws-sdk'
  ],
  output: {
    path: OUT_DIR,
    filename: 'index.js',
    libraryTarget: 'umd'
  },
  plugins: [
    new ZipPlugin({
      // OPTIONAL: defaults to the Webpack output path (above)
      // can be relative (to Webpack output path) or absolute
      path: 'zip' ,
      filename: 'lambda.zip',
    })
  ],
  mode: 'production'
};