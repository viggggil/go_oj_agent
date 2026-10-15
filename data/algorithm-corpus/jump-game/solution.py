n=int(input()); a=list(map(int,input().split())); far=0
for i,x in enumerate(a):
    if i>far: break
    far=max(far,i+x)
print('YES' if far>=n-1 else 'NO')
